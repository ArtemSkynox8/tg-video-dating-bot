package handlers

import (
	"context"
	"fmt"
	"html/template"
	"io"
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
	if platformUserID == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = miniRecordTemplate.Execute(w, map[string]string{
		"UserID": platformUserID,
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
	if duration > 60 {
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
	videoID, err := h.repo.SavePendingVideo(r.Context(), user.ID, publicURL, publicURL, duration)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	if _, err := h.max.SendMedia(context.Background(), user.PlatformChatID, publicURL, "Предпросмотр кружка", [][]maxapi.Button{
		{
			{Text: "✅ Сохранить", Payload: fmt.Sprintf("save_video:%d", videoID)},
			{Text: "🎥 Перезаписать", Payload: "rewrite_video"},
		},
	}); err != nil {
		_ = h.max.SendText(context.Background(), user.PlatformChatID, "Кружок загружен. Нажмите сохранить или перезапишите.", [][]maxapi.Button{
			{
				{Text: "✅ Сохранить", Payload: fmt.Sprintf("save_video:%d", videoID)},
				{Text: "🎥 Перезаписать", Payload: "rewrite_video"},
			},
		})
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
      width: min(78vw, 320px);
      aspect-ratio: 1;
      border-radius: 50%;
      overflow: hidden;
      background: #202b36;
      border: 5px solid rgba(255,255,255,.16);
      box-shadow: 0 22px 80px rgba(0,0,0,.35);
    }
    video {
      width: 100%;
      height: 100%;
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
    <p>Нажмите и удерживайте кнопку, чтобы записать кружок</p>
    <p id="status" class="status"></p>
  </main>
  <script>
    const userId = "{{.UserID}}";
    const preview = document.getElementById("preview");
    const button = document.getElementById("record");
    const timer = document.getElementById("timer");
    const statusEl = document.getElementById("status");
    let stream, recorder, chunks = [], startedAt = 0, tick, stopped = false;

    function setStatus(text) { statusEl.textContent = text; }
    function format(seconds) {
      return String(Math.floor(seconds / 60)).padStart(2, "0") + ":" + String(seconds % 60).padStart(2, "0");
    }
    async function init() {
      try {
        stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: "user" }, audio: true });
        preview.srcObject = stream;
      } catch (e) {
        setStatus("Разрешите доступ к камере и микрофону.");
      }
    }
    function start() {
      if (!stream || recorder?.state === "recording") return;
      chunks = [];
      stopped = false;
      recorder = new MediaRecorder(stream, { mimeType: "video/webm" });
      recorder.ondataavailable = e => { if (e.data.size) chunks.push(e.data); };
      recorder.onstop = upload;
      startedAt = Date.now();
      recorder.start();
      button.classList.add("recording");
      setStatus("");
      tick = setInterval(() => {
        const seconds = Math.floor((Date.now() - startedAt) / 1000);
        timer.textContent = format(seconds);
        if (seconds >= 60) stop();
      }, 200);
    }
    function stop() {
      if (!recorder || recorder.state !== "recording" || stopped) return;
      stopped = true;
      clearInterval(tick);
      button.classList.remove("recording");
      recorder.stop();
    }
    async function upload() {
      const duration = Math.round((Date.now() - startedAt) / 1000);
      if (duration < 3) {
        timer.textContent = "00:00";
        setStatus("Запись слишком короткая. Запишите минимум 3 секунды.");
        return;
      }
      setStatus("Загружаю видео...");
      const blob = new Blob(chunks, { type: "video/webm" });
      const form = new FormData();
      form.append("user_id", userId);
      form.append("duration", String(duration));
      form.append("video", blob, "circle.webm");
      const res = await fetch("/mini/upload", { method: "POST", body: form });
      if (!res.ok) {
        setStatus("Не удалось загрузить. Запишите заново.");
        return;
      }
      setStatus("Кружок сохранен.");
      setTimeout(() => {
        if (window.MAX && MAX.close) MAX.close();
        else window.close();
      }, 650);
    }
    button.addEventListener("pointerdown", start);
    button.addEventListener("pointerup", stop);
    button.addEventListener("pointerleave", stop);
    button.addEventListener("touchstart", e => { e.preventDefault(); start(); }, { passive: false });
    button.addEventListener("touchend", e => { e.preventDefault(); stop(); }, { passive: false });
    init();
  </script>
</body>
</html>`))
