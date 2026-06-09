package handlers

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/config"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/maxapi"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/repositories"
)

type MiniAppHandler struct {
	cfg  config.Config
	repo *repositories.Repository
	max  *maxapi.Client
}

func NewMiniAppHandler(cfg config.Config, repo *repositories.Repository, max *maxapi.Client) *MiniAppHandler {
	return &MiniAppHandler{cfg: cfg, repo: repo, max: max}
}

func (h *MiniAppHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /mini/record", h.recordPage)
	mux.HandleFunc("POST /mini/upload", h.upload)
	mux.Handle("/media/", http.StripPrefix("/media/", http.FileServer(http.Dir(h.cfg.UploadDir))))
	mux.Handle("/assets/recorder-theme/", http.StripPrefix("/assets/recorder-theme/", http.FileServer(http.Dir("assets/recorder-theme"))))
}

func (h *MiniAppHandler) recordPage(w http.ResponseWriter, r *http.Request) {
	platformUserID := strings.TrimSpace(r.URL.Query().Get("u"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Permissions-Policy", "camera=(self), microphone=(self)")
	w.Header().Set("Feature-Policy", "camera 'self'; microphone 'self'")
	_ = miniRecordTemplate.Execute(w, map[string]string{
		"UserID":       platformUserID,
		"ReturnToBot":  h.cfg.ReturnToBotURL,
	})
}

func (h *MiniAppHandler) upload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		http.Error(w, "bad upload", http.StatusBadRequest)
		return
	}
	platformUserID := strings.TrimSpace(r.FormValue("user_id"))
	if platformUserID == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}
	duration, _ := strconv.Atoi(r.FormValue("duration"))
	if duration > 0 && duration < 3 {
		http.Error(w, "too short", http.StatusBadRequest)
		return
	}
	if duration > 30 {
		http.Error(w, "too long", http.StatusBadRequest)
		return
	}

	user, err := h.repo.GetUserByPlatformID(r.Context(), platformUserID)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		http.Error(w, "missing video", http.StatusBadRequest)
		return
	}
	defer file.Close()

	if err := os.MkdirAll(h.cfg.UploadDir, 0o755); err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		ext = ".webm"
	}
	name := fmt.Sprintf("%d-%d%s", user.ID, time.Now().UnixNano(), ext)
	path := filepath.Join(h.cfg.UploadDir, name)
	out, err := os.Create(path)
	if err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, file); err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	uploadPath := path
	uploadName := name
	if convertedPath, err := convertVideoForMax(r.Context(), path, duration); err != nil {
		log.Printf("convert video for max user=%s path=%s: %v", user.PlatformUserID, path, err)
	} else {
		uploadPath = convertedPath
		uploadName = filepath.Base(convertedPath)
	}

	publicURL := strings.TrimRight(h.cfg.PublicBaseURL, "/") + "/media/" + uploadName
	platformMediaID := publicURL
	uploadedToMax := false
	if token, err := h.max.UploadVideo(r.Context(), uploadPath); err != nil {
		log.Printf("upload video to max user=%s path=%s: %v", user.PlatformUserID, uploadPath, err)
	} else {
		platformMediaID = token
		uploadedToMax = true
	}
	videoID, err := h.repo.SavePendingVideo(r.Context(), user.ID, platformMediaID, publicURL, duration)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	buttons := [][]maxapi.Button{
		{
			{Text: "✅ Сохранить", Payload: fmt.Sprintf("save_video:%d", videoID)},
			{Text: "🎥 Перезаписать", Payload: "rewrite_video"},
		},
	}
	var sendErr error
	if uploadedToMax {
		for attempt := 0; attempt < 6; attempt++ {
			if attempt > 0 {
				time.Sleep(5 * time.Second)
			}
			_, sendErr = h.max.SendVideoThenTextToDialogOrUser(context.Background(), user.PlatformDialogID, user.PlatformChatID, platformMediaID, "Предпросмотр кружка", buttons)
			if sendErr == nil {
				break
			}
			log.Printf("send max video preview attempt=%d user=%s token=%s: %v", attempt+1, user.PlatformUserID, platformMediaID, sendErr)
		}
	} else {
		sendErr = fmt.Errorf("video was not uploaded to max")
	}
	if sendErr != nil {
		log.Printf("send video preview failed user=%s video_id=%d: %v", user.PlatformUserID, videoID, sendErr)
		_ = h.max.SendText(context.Background(), user.PlatformChatID, "Кружок загружен. MAX пока не прислал видео в чат, но файл сохранен на сервере:\n"+publicURL+"\n\nНажмите сохранить или перезапишите.", buttons)
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func convertVideoForMax(ctx context.Context, inputPath string, duration int) (string, error) {
	ext := filepath.Ext(inputPath)
	outputPath := strings.TrimSuffix(inputPath, ext) + "-max.mp4"
	convertCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	limit := duration
	if limit <= 0 || limit > 30 {
		limit = 30
	}
	cmd := exec.CommandContext(convertCtx, "ffmpeg",
		"-y",
		"-fflags", "+genpts",
		"-i", inputPath,
		"-map", "0:v:0",
		"-map", "0:a?",
		"-t", strconv.Itoa(limit),
		"-vf", `fps=30,crop=min(iw\,ih):min(iw\,ih),scale=480:480,setsar=1,setpts=N/(30*TB),format=yuv420p`,
		"-af", "aresample=async=1:first_pts=0",
		"-c:v", "libx264",
		"-profile:v", "baseline",
		"-level", "3.0",
		"-crf", "22",
		"-preset", "veryfast",
		"-bf", "0",
		"-r", "30",
		"-c:a", "aac",
		"-b:a", "128k",
		"-shortest",
		"-avoid_negative_ts", "make_zero",
		"-movflags", "+faststart",
		outputPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	logConvertedVideo(ctx, outputPath)
	return outputPath, nil
}

func logConvertedVideo(ctx context.Context, path string) {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, "ffprobe",
		"-v", "error",
		"-show_entries", "stream=codec_type,codec_name",
		"-of", "compact=p=0:nk=1",
		path,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("probe converted video path=%s: %v: %s", path, err, strings.TrimSpace(string(output)))
		return
	}
	log.Printf("converted video streams path=%s streams=%q", path, strings.TrimSpace(string(output)))
}

var miniRecordTemplate = template.Must(template.New("mini-record").Parse(`<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
  <title>Запись кружка</title>
  <script src="https://st.max.ru/js/max-web-app.js"></script>
  <style>
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      display: grid;
      place-items: center;
      background: #101820;
      color: #fff;
      font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    main {
      width: min(100vw, 480px);
      min-height: 100vh;
      padding: 28px 20px;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      gap: 22px;
    }
    .preview {
      --progress: 0%;
      width: min(78vw, 320px);
      aspect-ratio: 1;
      padding: 7px;
      position: relative;
      border-radius: 50%;
      overflow: hidden;
      background:
        conic-gradient(#51d4ff var(--progress), rgba(255,255,255,.16) 0),
        #202b36;
      box-shadow: 0 22px 80px rgba(0,0,0,.35);
    }
    .preview::before {
      content: "";
      position: absolute;
      inset: 7px;
      border-radius: 50%;
      background: #202b36 url("/assets/recorder-theme/dark.jpg") center / cover no-repeat;
      filter: blur(15px);
      transform: scale(1.12);
      opacity: .9;
      z-index: 0;
    }
    @media (prefers-color-scheme: light) {
      .preview::before {
        background-image: url("/assets/recorder-theme/light.jpg");
      }
    }
    video {
      position: relative;
      z-index: 1;
      width: 100%;
      height: 100%;
      border-radius: 50%;
      background: #202b36;
      object-fit: cover;
      transform: scaleX(-1);
    }
    .timer {
      font-size: 32px;
      font-weight: 700;
      letter-spacing: 0;
    }
    .record {
      width: 96px;
      height: 96px;
      border-radius: 50%;
      border: 7px solid #fff;
      background: #ee3f4d;
      box-shadow: 0 12px 36px rgba(238,63,77,.38);
      touch-action: none;
    }
    .record.recording {
      background: #ff6f76;
      transform: scale(.94);
    }
    .fallback {
      min-height: 44px;
      padding: 0 18px;
      border: 1px solid rgba(255,255,255,.2);
      border-radius: 12px;
      background: rgba(255,255,255,.08);
      color: #fff;
      font: inherit;
      font-weight: 700;
      display: none;
    }
    .fallback.show {
      display: inline-flex;
      align-items: center;
    }
    p {
      margin: 0;
      max-width: 320px;
      color: rgba(255,255,255,.78);
      text-align: center;
      font-size: 16px;
      line-height: 1.35;
    }
    .status { min-height: 22px; color: #83e2b7; }
    .overlay {
      position: fixed;
      inset: 0;
      display: none;
      place-items: center;
      padding: 24px;
      background: rgba(7, 12, 18, .82);
      backdrop-filter: blur(14px);
      z-index: 10;
    }
    .overlay.open { display: grid; }
    .modal {
      width: min(100%, 360px);
      padding: 24px;
      border-radius: 18px;
      background: #fff;
      color: #17202a;
      display: grid;
      gap: 16px;
      text-align: center;
      box-shadow: 0 26px 90px rgba(0,0,0,.35);
    }
    .modal h2 {
      margin: 0;
      font-size: 24px;
      letter-spacing: 0;
    }
    .modal p {
      color: #5b6672;
      max-width: none;
      font-size: 15px;
    }
    .bar {
      height: 12px;
      border-radius: 999px;
      overflow: hidden;
      background: #e8edf3;
    }
    .bar span {
      display: block;
      width: 0%;
      height: 100%;
      border-radius: inherit;
      background: linear-gradient(90deg, #39c9d0, #7376ff);
      transition: width .18s ease;
    }
    .percent {
      font-size: 34px;
      font-weight: 800;
      letter-spacing: 0;
    }
    .return {
      min-height: 54px;
      border: 0;
      border-radius: 14px;
      background: #1683ff;
      color: #fff;
      font: inherit;
      font-weight: 700;
      display: none;
      align-items: center;
      justify-content: center;
      text-decoration: none;
    }
    .return.ready { display: flex; }
  </style>
</head>
<body>
  <main>
    <div class="preview"><video id="preview" autoplay muted playsinline></video></div>
    <div id="timer" class="timer">00:00</div>
    <button id="record" class="record" aria-label="Записать"></button>
    <button id="fallbackButton" class="fallback" type="button">Выбрать видео</button>
    <input id="fallbackFile" type="file" accept="video/*,video/mp4,video/webm" capture="user" hidden>
    <p>Нажмите и удерживайте кнопку, чтобы записать кружок</p>
    <p id="status" class="status"></p>
  </main>
  <div id="overlay" class="overlay" aria-hidden="true">
    <div class="modal" role="dialog" aria-modal="true">
      <h2 id="modalTitle">Готовим вашу анкету</h2>
      <p id="modalText">Загружаем кружок и отправляем предпросмотр в бот.</p>
      <div class="bar"><span id="uploadBar"></span></div>
      <div id="uploadPercent" class="percent">0%</div>
      <a id="returnButton" class="return" href="{{.ReturnToBot}}" target="_self">Вернуться в бота</a>
    </div>
  </div>
  <script>
    function resolveUserId() {
      const fallback = "{{.UserID}}";
      const unsafe = window.WebApp && window.WebApp.initDataUnsafe;
      const bridgeUser = unsafe && unsafe.user && unsafe.user.id;
      if (bridgeUser) return String(bridgeUser);
      const startParam = unsafe && (unsafe.start_param || unsafe.startParam);
      if (/^\d+$/.test(String(startParam || ""))) return String(startParam);
      const fromInitData = userIdFromInitData(window.WebApp && window.WebApp.initData);
      if (fromInitData) return fromInitData;
      const fromHash = userIdFromHash();
      if (fromHash) return fromHash;
      return fallback;
    }
    function userIdFromInitData(initData) {
      if (!initData) return "";
      try {
        const params = new URLSearchParams(initData);
        const user = JSON.parse(params.get("user") || "{}");
        if (user && user.id) return String(user.id);
        const startParam = params.get("start_param");
        if (/^\d+$/.test(String(startParam || ""))) return String(startParam);
      } catch (e) {
        console.warn("cannot parse initData", e);
      }
      return "";
    }
    function userIdFromHash() {
      try {
        const hash = new URLSearchParams(window.location.hash.replace(/^#/, ""));
        const webAppData = hash.get("WebAppData");
        if (!webAppData) return "";
        return userIdFromInitData(decodeURIComponent(webAppData));
      } catch (e) {
        console.warn("cannot parse WebAppData hash", e);
      }
      return "";
    }
    const userId = resolveUserId();
    const returnToBotURL = "{{.ReturnToBot}}";
    const preview = document.getElementById("preview");
    const previewRing = document.querySelector(".preview");
    const button = document.getElementById("record");
    const fallbackButton = document.getElementById("fallbackButton");
    const fallbackFile = document.getElementById("fallbackFile");
    const timer = document.getElementById("timer");
    const statusEl = document.getElementById("status");
    const overlay = document.getElementById("overlay");
    const uploadBar = document.getElementById("uploadBar");
    const uploadPercent = document.getElementById("uploadPercent");
    const modalTitle = document.getElementById("modalTitle");
    const modalText = document.getElementById("modalText");
    const returnButton = document.getElementById("returnButton");
    const maxDuration = 30;
    let stream, recorder, chunks = [], startedAt = 0, tick = 0, drawTick = 0, stopped = false, holding = false, starting = false;
    const chatBg = new Image();
    chatBg.src = window.matchMedia && window.matchMedia("(prefers-color-scheme: light)").matches
      ? "/assets/recorder-theme/light.jpg"
      : "/assets/recorder-theme/dark.jpg";

    function setStatus(text) { statusEl.textContent = text; }
    function format(seconds) {
      return String(Math.floor(seconds / 60)).padStart(2, "0") + ":" + String(seconds % 60).padStart(2, "0");
    }
    function setProgress(seconds) {
      const clamped = Math.max(0, Math.min(maxDuration, seconds));
      previewRing.style.setProperty("--progress", String((clamped / maxDuration) * 100) + "%");
    }
    function setUploadProgress(value) {
      const clamped = Math.max(0, Math.min(100, Math.round(value)));
      uploadBar.style.width = clamped + "%";
      uploadPercent.textContent = clamped + "%";
    }
    function showPreparing() {
      overlay.classList.add("open");
      overlay.setAttribute("aria-hidden", "false");
      modalTitle.textContent = "Готовим вашу анкету";
      modalText.textContent = "Загружаем кружок и отправляем предпросмотр в бот.";
      returnButton.classList.remove("ready");
      setUploadProgress(0);
    }
    function showReady() {
      modalTitle.textContent = "Анкета готова";
      modalText.textContent = "Кружок загружен. Вернитесь в бот и выберите, сохранить его или перезаписать.";
      setUploadProgress(100);
      returnButton.href = returnToBotURL || "https://max.ru/id550411830268_1_bot";
      returnButton.classList.add("ready");
    }
    async function init() {
      if (window.WebApp && WebApp.ready) WebApp.ready();
      if (!userId) {
        setStatus("Откройте запись из кнопки бота в MAX.");
        button.disabled = true;
        return;
      }
      setStatus("Нажмите красную кнопку, чтобы разрешить камеру.");
    }
    function getUserMediaCompat(constraints) {
      if (navigator.mediaDevices && navigator.mediaDevices.getUserMedia) {
        return navigator.mediaDevices.getUserMedia(constraints);
      }
      const legacy = navigator.getUserMedia || navigator.webkitGetUserMedia || navigator.mozGetUserMedia;
      if (!legacy) return Promise.reject(new Error("getUserMedia is not supported"));
      return new Promise((resolve, reject) => legacy.call(navigator, constraints, resolve, reject));
    }
    async function requestFirstAvailableMedia(constraintsList) {
      let lastError = null;
      for (const constraints of constraintsList) {
        try {
          return await getUserMediaCompat(constraints);
        } catch (error) {
          lastError = error;
          console.warn("media constraints failed", constraints, error);
        }
      }
      throw lastError || new Error("media request failed");
    }
    function mediaErrorLabel(error) {
      if (!navigator.mediaDevices && !navigator.getUserMedia && !navigator.webkitGetUserMedia && !navigator.mozGetUserMedia) {
        return "unsupported";
      }
      if (!error) return "unknown";
      return error.name || error.message || "unknown";
    }
    async function ensureStream() {
      if (stream) return true;
      try {
        stream = await getUserMediaCompat({ video: { facingMode: "user" }, audio: false });
        try {
          const audioStream = await getUserMediaCompat({ audio: true, video: false });
          audioStream.getAudioTracks().forEach(track => stream.addTrack(track));
        } catch (audioError) {
          console.warn("audio permission failed", audioError);
        }
        preview.srcObject = stream;
        setStatus("");
        return true;
      } catch (e) {
        console.warn("camera permission failed", e);
        setStatus("MAX не открыл встроенную камеру. Откроется системная запись видео.");
        fallbackFile.click();
        return false;
      }
    }
    async function ensureAudioVideoStream() {
      if (stream) return true;
      try {
        stream = await requestFirstAvailableMedia([
          { video: { facingMode: { ideal: "user" } }, audio: false },
          { video: true, audio: false }
        ]);
        try {
          const audioStream = await requestFirstAvailableMedia([
            { audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true }, video: false },
            { audio: true, video: false }
          ]);
          audioStream.getAudioTracks().forEach(track => stream.addTrack(track));
        } catch (audioError) {
          console.warn("audio permission failed", audioError);
        }
        preview.srcObject = stream;
        setStatus(stream.getAudioTracks().length ? "" : "Микрофон не подключился.");
        return true;
      } catch (videoError) {
        const reason = mediaErrorLabel(videoError);
        console.warn("camera permission failed", videoError);
        setStatus("MAX не открыл встроенную камеру: " + reason + ". Откроется системная запись видео.");
        fallbackButton.classList.add("show");
        fallbackFile.click();
        return false;
      }
    }
    function buildCircleStream() {
      const canvas = document.createElement("canvas");
      canvas.width = 720;
      canvas.height = 720;
      const ctx = canvas.getContext("2d");
      function drawBackground() {
        ctx.fillStyle = "#8ccff4";
        ctx.fillRect(0, 0, canvas.width, canvas.height);
        if (!chatBg.complete || !chatBg.naturalWidth) return;
        const imageSide = Math.min(chatBg.naturalWidth, chatBg.naturalHeight);
        const sx = (chatBg.naturalWidth - imageSide) / 2;
        const sy = (chatBg.naturalHeight - imageSide) / 2;
        ctx.save();
        ctx.filter = "blur(14px)";
        ctx.globalAlpha = .88;
        ctx.drawImage(chatBg, sx, sy, imageSide, imageSide, -28, -28, canvas.width + 56, canvas.height + 56);
        ctx.restore();
      }
      const draw = () => {
        drawBackground();
        const vw = preview.videoWidth || 720;
        const vh = preview.videoHeight || 720;
        const side = Math.min(vw, vh);
        const sx = (vw - side) / 2;
        const sy = (vh - side) / 2;
        ctx.save();
        ctx.beginPath();
        ctx.arc(canvas.width / 2, canvas.height / 2, canvas.width / 2 - 8, 0, Math.PI * 2);
        ctx.clip();
        ctx.translate(canvas.width, 0);
        ctx.scale(-1, 1);
        ctx.drawImage(preview, sx, sy, side, side, 0, 0, canvas.width, canvas.height);
        ctx.restore();
        drawTick = requestAnimationFrame(draw);
      };
      draw();
      if (!canvas.captureStream) {
        cancelAnimationFrame(drawTick);
        return stream;
      }
      const canvasStream = canvas.captureStream(30);
      stream.getAudioTracks().forEach(track => canvasStream.addTrack(track));
      return canvasStream;
    }
    function startProgressLoop() {
      cancelAnimationFrame(tick);
      const frame = () => {
        if (!startedAt || stopped) return;
        const elapsed = (Date.now() - startedAt) / 1000;
        timer.textContent = format(elapsed);
        setProgress(elapsed);
        if (elapsed >= maxDuration) {
          stop();
          return;
        }
        tick = requestAnimationFrame(frame);
      };
      tick = requestAnimationFrame(frame);
    }
    async function start() {
      holding = true;
      if (starting || recorder?.state === "recording") return;
      starting = true;
      const ok = stream ? true : await ensureAudioVideoStream();
      starting = false;
      if (!ok || !holding) return;
      chunks = [];
      stopped = false;
      const recordingStream = buildCircleStream();
      const mimeType = MediaRecorder.isTypeSupported("video/webm;codecs=vp8,opus")
        ? "video/webm;codecs=vp8,opus"
        : (MediaRecorder.isTypeSupported("video/webm") ? "video/webm" : "");
      recorder = mimeType ? new MediaRecorder(recordingStream, { mimeType }) : new MediaRecorder(recordingStream);
      recorder.ondataavailable = e => { if (e.data.size) chunks.push(e.data); };
      recorder.onstop = upload;
      startedAt = Date.now();
      recorder.start();
      button.classList.add("recording");
      setStatus("");
      setProgress(0);
      startProgressLoop();
    }
    function stop() {
      holding = false;
      if (!recorder || recorder.state !== "recording" || stopped) return;
      stopped = true;
      cancelAnimationFrame(tick);
      cancelAnimationFrame(drawTick);
      button.classList.remove("recording");
      setProgress(Math.min(maxDuration, (Date.now() - startedAt) / 1000));
      recorder.stop();
    }
    async function upload() {
      const duration = Math.round((Date.now() - startedAt) / 1000);
      if (duration < 3) {
        timer.textContent = "00:00";
        setProgress(0);
        setStatus("Запись слишком короткая. Запишите минимум 3 секунды.");
        return;
      }
      await uploadBlob(new Blob(chunks, { type: "video/webm" }), duration, "circle.webm");
    }
    async function uploadBlob(blob, duration, filename) {
      setStatus("Загружаю видео...");
      showPreparing();
      const form = new FormData();
      form.append("user_id", userId);
      form.append("duration", String(duration));
      form.append("video", blob, filename);
      try {
        await sendUpload(form);
      } catch (e) {
        console.warn("upload failed", e);
        setStatus("Не удалось загрузить. Запишите заново.");
        modalTitle.textContent = "Не удалось загрузить";
        modalText.textContent = "Закройте это окно и попробуйте записать кружок заново.";
        returnButton.href = returnToBotURL || "https://max.ru/id550411830268_1_bot";
        returnButton.classList.add("ready");
        return;
      }
      setStatus("Кружок сохранен.");
      showReady();
    }
    function sendUpload(form) {
      return new Promise((resolve, reject) => {
        const xhr = new XMLHttpRequest();
        let serverWait = 0;
        let serverTimer = 0;
        xhr.open("POST", "/mini/upload", true);
        xhr.upload.onprogress = e => {
          if (!e.lengthComputable) return;
          setUploadProgress(Math.min(90, (e.loaded / e.total) * 90));
        };
        xhr.upload.onload = () => {
          modalText.textContent = "Видео загружено. MAX обрабатывает предпросмотр.";
          serverTimer = setInterval(() => {
            serverWait = Math.min(98, serverWait + 1);
            setUploadProgress(Math.max(90, serverWait));
          }, 550);
        };
        xhr.onload = () => {
          clearInterval(serverTimer);
          if (xhr.status >= 200 && xhr.status < 300) {
            resolve();
          } else {
            reject(new Error("upload status " + xhr.status));
          }
        };
        xhr.onerror = () => {
          clearInterval(serverTimer);
          reject(new Error("network error"));
        };
        xhr.send(form);
      });
    }
    returnButton.addEventListener("click", () => {
      setTimeout(() => {
        window.close();
      }, 500);
    });
    fallbackFile.addEventListener("change", async () => {
      holding = false;
      const file = fallbackFile.files && fallbackFile.files[0];
      if (!file) {
        setStatus("Видео не выбрано. Нажмите красную кнопку еще раз.");
        return;
      }
      await uploadBlob(file, 0, file.name || "circle.mp4");
    });
    fallbackButton.addEventListener("click", () => fallbackFile.click());
    button.addEventListener("pointerdown", start);
    button.addEventListener("pointerup", stop);
    button.addEventListener("pointerleave", stop);
    button.addEventListener("touchstart", e => { e.preventDefault(); start(); }, { passive: false });
    button.addEventListener("touchend", e => { e.preventDefault(); stop(); }, { passive: false });
    init();
  </script>
</body>
</html>`))
