package handlers

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
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

	publicURL := strings.TrimRight(h.cfg.PublicBaseURL, "/") + "/media/" + name
	platformMediaID := publicURL
	uploadedToMax := false
	if token, err := h.max.UploadVideo(r.Context(), path); err != nil {
		log.Printf("upload video to max user=%s path=%s: %v", user.PlatformUserID, path, err)
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
		for attempt := 0; attempt < 4; attempt++ {
			if attempt > 0 {
				time.Sleep(time.Duration(attempt*2) * time.Second)
			}
			_, sendErr = h.max.SendMedia(context.Background(), user.PlatformChatID, platformMediaID, "Предпросмотр кружка", buttons)
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
      border-radius: 50%;
      overflow: hidden;
      background:
        conic-gradient(#51d4ff var(--progress), rgba(255,255,255,.16) 0),
        #202b36;
      box-shadow: 0 22px 80px rgba(0,0,0,.35);
    }
    video {
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
    p {
      margin: 0;
      max-width: 320px;
      color: rgba(255,255,255,.78);
      text-align: center;
      font-size: 16px;
      line-height: 1.35;
    }
    .status { min-height: 22px; color: #83e2b7; }
  </style>
</head>
<body>
  <main>
    <div class="preview"><video id="preview" autoplay muted playsinline></video></div>
    <div id="timer" class="timer">00:00</div>
    <button id="record" class="record" aria-label="Записать"></button>
    <input id="fallbackFile" type="file" accept="video/*,video/mp4,video/webm" capture="user" hidden>
    <p>Нажмите и удерживайте кнопку, чтобы записать кружок</p>
    <p id="status" class="status"></p>
  </main>
  <script>
    function resolveUserId() {
      const fallback = "{{.UserID}}";
      const unsafe = window.WebApp && window.WebApp.initDataUnsafe;
      const bridgeUser = unsafe && unsafe.user && unsafe.user.id;
      if (bridgeUser) return String(bridgeUser);
      return fallback;
    }
    const userId = resolveUserId();
    const returnToBotURL = "{{.ReturnToBot}}";
    const preview = document.getElementById("preview");
    const previewRing = document.querySelector(".preview");
    const button = document.getElementById("record");
    const fallbackFile = document.getElementById("fallbackFile");
    const timer = document.getElementById("timer");
    const statusEl = document.getElementById("status");
    const maxDuration = 30;
    let stream, recorder, chunks = [], startedAt = 0, tick = 0, drawTick = 0, stopped = false, holding = false, starting = false;

    function setStatus(text) { statusEl.textContent = text; }
    function format(seconds) {
      return String(Math.floor(seconds / 60)).padStart(2, "0") + ":" + String(seconds % 60).padStart(2, "0");
    }
    function setProgress(seconds) {
      const clamped = Math.max(0, Math.min(maxDuration, seconds));
      previewRing.style.setProperty("--progress", String((clamped / maxDuration) * 100) + "%");
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
    function buildCircleStream() {
      const canvas = document.createElement("canvas");
      canvas.width = 720;
      canvas.height = 720;
      const ctx = canvas.getContext("2d");
      const draw = () => {
        ctx.fillStyle = "#101820";
        ctx.fillRect(0, 0, canvas.width, canvas.height);
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
      const ok = await ensureStream();
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
      const form = new FormData();
      form.append("user_id", userId);
      form.append("duration", String(duration));
      form.append("video", blob, filename);
      const res = await fetch("/mini/upload", { method: "POST", body: form });
      if (!res.ok) {
        setStatus("Не удалось загрузить. Запишите заново.");
        return;
      }
      setStatus("Кружок сохранен.");
      setTimeout(() => {
        returnToBot();
      }, 500);
    }
    function returnToBot() {
      if (history.length > 1) {
        history.back();
      }
      setTimeout(() => {
        if (window.WebApp && WebApp.close) {
          WebApp.close();
          return;
        }
        if (window.MAX && MAX.close) {
          MAX.close();
          return;
        }
        window.close();
        setTimeout(() => {
          window.location.href = returnToBotURL || "https://max.ru/id550411830268_1_bot";
        }, 250);
      }, 250);
    }
    fallbackFile.addEventListener("change", async () => {
      holding = false;
      const file = fallbackFile.files && fallbackFile.files[0];
      if (!file) {
        setStatus("Видео не выбрано. Нажмите красную кнопку еще раз.");
        return;
      }
      await uploadBlob(file, 0, file.name || "circle.mp4");
    });
    button.addEventListener("pointerdown", start);
    button.addEventListener("pointerup", stop);
    button.addEventListener("pointerleave", stop);
    button.addEventListener("touchstart", e => { e.preventDefault(); start(); }, { passive: false });
    button.addEventListener("touchend", e => { e.preventDefault(); stop(); }, { passive: false });
    init();
  </script>
</body>
</html>`))
