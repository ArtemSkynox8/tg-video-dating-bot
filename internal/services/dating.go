package services

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/maxapi"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/models"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/repositories"
)

type DatingService struct {
	repo *repositories.Repository
	max  *maxapi.Client
}

func NewDatingService(repo *repositories.Repository, max *maxapi.Client) *DatingService {
	return &DatingService{repo: repo, max: max}
}

func (s *DatingService) HandleMessage(ctx context.Context, msg maxapi.MessageUpdate) error {
	user, err := s.repo.UpsertPlatformUser(ctx, models.User{
		PlatformUserID: msg.From.ID,
		PlatformChatID: msg.Chat.ID,
		ProfileLink:    msg.From.ProfileLink,
		Username:       msg.From.Username,
	})
	if err != nil {
		return err
	}

	text := strings.TrimSpace(msg.Text)
	switch {
	case text == "/start":
		return s.max.SendText(ctx, msg.Chat.ID, "Привет. Заполним анкету: отправьте имя от 2 до 30 символов.", nil)
	case strings.HasPrefix(text, "📬 Взаимные лайки"):
		return s.SendMatches(ctx, *user)
	case text == "▶️ Начать просмотр" || text == "/browse":
		return s.SendNextCandidate(ctx, *user)
	case len(msg.Media) > 0:
		return s.HandleMedia(ctx, *user, msg.Media[0])
	case user.Name == "":
		return s.SaveNameStep(ctx, *user, text)
	case user.Gender == "":
		return s.SaveGenderStep(ctx, *user, text)
	case user.PreferredGender == "":
		return s.SavePreferredGenderStep(ctx, *user, text)
	default:
		return s.max.SendText(ctx, msg.Chat.ID, "Выберите действие в меню или нажмите ▶️ Начать просмотр.", mainMenuButtons())
	}
}

func (s *DatingService) HandleCallback(ctx context.Context, cb maxapi.CallbackUpdate) error {
	user, err := s.repo.GetUserByPlatformID(ctx, cb.From.ID)
	if err != nil {
		return err
	}
	parts := strings.Split(cb.Payload, ":")
	if len(parts) < 1 {
		return nil
	}
	switch parts[0] {
	case "browse":
		return s.SendNextCandidate(ctx, *user)
	case "gender":
		if len(parts) != 2 {
			return nil
		}
		return s.SaveGenderStep(ctx, *user, parts[1])
	case "preferred":
		if len(parts) != 2 {
			return nil
		}
		return s.SavePreferredGenderStep(ctx, *user, parts[1])
	case "like", "next":
		if len(parts) != 3 {
			return nil
		}
		action := models.ActionNext
		if parts[0] == "like" {
			action = models.ActionLike
		}
		return s.HandleBrowseAction(ctx, *user, cb.Chat.ID, cb.MessageID, parts[1], parts[2], action)
	case "report":
		if len(parts) != 3 {
			return nil
		}
		return s.max.SendText(ctx, cb.Chat.ID, "Выберите причину жалобы:", reportButtons(parts[1], parts[2]))
	case "report_reason":
		if len(parts) != 4 {
			return nil
		}
		return s.HandleReport(ctx, *user, cb.Chat.ID, cb.MessageID, parts[1], parts[2], parts[3])
	case "matches":
		return s.SendMatches(ctx, *user)
	case "rewrite_video":
		return s.max.SendText(ctx, cb.Chat.ID, "Отправьте новое короткое видео до 60 секунд. Старое видео станет неактивным.", nil)
	case "edit_profile":
		return s.max.SendText(ctx, cb.Chat.ID, "Отправьте новое имя. После этого можно будет обновить пол и предпочтения.", nil)
	case "premium":
		return s.max.SendText(ctx, cb.Chat.ID, "💎 Premium будет подключен отдельным модулем после проверки платежей MAX.", nil)
	case "menu_report":
		return s.max.SendText(ctx, cb.Chat.ID, "Жалобы из меню доступны только на пользователей из взаимных лайков. Откройте 📬 Взаимные лайки и выберите контакт.", nil)
	}
	return nil
}

func (s *DatingService) SaveNameStep(ctx context.Context, user models.User, name string) error {
	if !validName(name) {
		return s.max.SendText(ctx, user.PlatformChatID, "Имя должно быть от 2 до 30 символов: буквы, пробелы и дефисы.", nil)
	}
	if err := s.repo.UpdateProfile(ctx, user.ID, name, "", ""); err != nil {
		return err
	}
	return s.max.SendText(ctx, user.PlatformChatID, "Выберите свой пол:", genderButtons())
}

func (s *DatingService) SaveGenderStep(ctx context.Context, user models.User, gender string) error {
	gender = normalizeGender(gender)
	if gender == "" {
		return s.max.SendText(ctx, user.PlatformChatID, "Выберите: Мужской или Женский.", genderButtons())
	}
	if err := s.repo.UpdateProfile(ctx, user.ID, user.Name, gender, user.PreferredGender); err != nil {
		return err
	}
	return s.max.SendText(ctx, user.PlatformChatID, "Какие видео хотите получать?", preferredButtons())
}

func (s *DatingService) SavePreferredGenderStep(ctx context.Context, user models.User, preferred string) error {
	preferred = normalizePreferredGender(preferred)
	if preferred == "" {
		return s.max.SendText(ctx, user.PlatformChatID, "Выберите: Мужские, Женские или Не важно.", preferredButtons())
	}
	if err := s.repo.UpdateProfile(ctx, user.ID, user.Name, user.Gender, preferred); err != nil {
		return err
	}
	return s.max.SendText(ctx, user.PlatformChatID, "Отправьте короткое круглое видео до 60 секунд.", nil)
}

func (s *DatingService) HandleMedia(ctx context.Context, user models.User, media maxapi.Media) error {
	if media.Type != "video" && media.Type != "round_video" {
		return s.max.SendText(ctx, user.PlatformChatID, "Принимается только поддерживаемое короткое видео MAX.", nil)
	}
	if media.Duration > 60 {
		return s.max.SendText(ctx, user.PlatformChatID, "Видео должно быть не длиннее 60 секунд.", nil)
	}
	if err := s.repo.SaveVideo(ctx, user.ID, media.ID, media.URL, media.Duration); err != nil {
		return err
	}
	return s.max.SendText(ctx, user.PlatformChatID, "✅ Анкета создана. Теперь вы можете смотреть видео других пользователей.", [][]maxapi.Button{
		{{Text: "▶️ Начать просмотр", Payload: "browse"}},
	})
}

func (s *DatingService) SendNextCandidate(ctx context.Context, user models.User) error {
	candidate, err := s.repo.FindCandidate(ctx, user.ID)
	if err != nil {
		if err == repositories.ErrNotFound {
			return s.max.SendText(ctx, user.PlatformChatID, "Пока нет подходящих видео. Загляните позже.", mainMenuButtons())
		}
		return err
	}
	_, err = s.max.SendMedia(ctx, user.PlatformChatID, candidate.PlatformMediaID, candidate.Owner.Name, browseButtons(candidate.ID, candidate.Owner.ID))
	return err
}

func (s *DatingService) HandleBrowseAction(ctx context.Context, user models.User, chatID, messageID, videoIDText, ownerIDText, action string) error {
	videoID, ownerID := parseID(videoIDText), parseID(ownerIDText)
	if videoID == 0 || ownerID == 0 {
		return nil
	}
	_ = s.max.DeleteMessage(ctx, chatID, messageID)
	if err := s.repo.CreateView(ctx, user.ID, videoID, ownerID, action); err != nil {
		return err
	}
	if action == models.ActionLike {
		if _, err := s.repo.CreateLike(ctx, user.ID, ownerID); err != nil {
			return err
		}
		if err := s.repo.EnqueuePriority(ctx, ownerID, user.ID); err != nil {
			return err
		}
		reverse, err := s.repo.HasReverseLike(ctx, user.ID, ownerID)
		if err != nil {
			return err
		}
		if reverse {
			if err := s.repo.CreateMatch(ctx, user.ID, ownerID); err != nil {
				return err
			}
			return s.max.SendText(ctx, chatID, "❤️ У вас новый взаимный лайк!", [][]maxapi.Button{
				{{Text: "📬 Взаимные лайки", Payload: "matches"}},
				{{Text: "▶️ Продолжить просмотр", Payload: "browse"}},
			})
		}
	}
	return s.SendNextCandidate(ctx, user)
}

func (s *DatingService) HandleReport(ctx context.Context, user models.User, chatID, messageID, videoIDText, ownerIDText, reason string) error {
	videoID, ownerID := parseID(videoIDText), parseID(ownerIDText)
	if videoID == 0 || ownerID == 0 {
		return nil
	}
	_ = s.max.DeleteMessage(ctx, chatID, messageID)
	if err := s.repo.CreateView(ctx, user.ID, videoID, ownerID, models.ActionReport); err != nil {
		return err
	}
	if err := s.repo.CreateVideoReport(ctx, user.ID, videoID, ownerID, reason); err != nil {
		return err
	}
	if err := s.repo.ApplyReportRestrictions(ctx, ownerID); err != nil {
		return err
	}
	return s.SendNextCandidate(ctx, user)
}

func (s *DatingService) SendMatches(ctx context.Context, user models.User) error {
	users, err := s.repo.ListVisibleMatches(ctx, user.ID)
	if err != nil {
		return err
	}
	if len(users) == 0 {
		return s.max.SendText(ctx, user.PlatformChatID, "У вас пока нет взаимных лайков.", mainMenuButtons())
	}
	lines := []string{"📬 Взаимные лайки:"}
	for _, u := range users {
		name := u.Name
		if u.ProfileLink != "" {
			name = fmt.Sprintf("%s (%s)", name, u.ProfileLink)
		}
		lines = append(lines, fmt.Sprintf("%s | 💬 Написать | 🎥 Смотреть видео | 🗑 Удалить из списка", name))
	}
	return s.max.SendText(ctx, user.PlatformChatID, strings.Join(lines, "\n"), mainMenuButtons())
}

var nameRe = regexp.MustCompile(`^[\p{L} -]{2,30}$`)

func validName(name string) bool {
	return nameRe.MatchString(strings.TrimSpace(name))
}

func normalizeGender(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "male", "мужской", "мужские":
		return models.GenderMale
	case "female", "женский", "женские":
		return models.GenderFemale
	default:
		return ""
	}
}

func normalizePreferredGender(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "male", "мужской", "мужские":
		return models.GenderMale
	case "female", "женский", "женские":
		return models.GenderFemale
	case "any", "не важно":
		return models.GenderAny
	default:
		return ""
	}
}

func parseID(value string) int64 {
	var out int64
	_, _ = fmt.Sscan(value, &out)
	return out
}

func genderButtons() [][]maxapi.Button {
	return [][]maxapi.Button{{{Text: "Мужской", Payload: "gender:male"}, {Text: "Женский", Payload: "gender:female"}}}
}

func preferredButtons() [][]maxapi.Button {
	return [][]maxapi.Button{{{Text: "Мужские", Payload: "preferred:male"}, {Text: "Женские", Payload: "preferred:female"}, {Text: "Не важно", Payload: "preferred:any"}}}
}

func browseButtons(videoID, ownerID int64) [][]maxapi.Button {
	return [][]maxapi.Button{{
		{Text: "❤️ Написать", Payload: fmt.Sprintf("like:%d:%d", videoID, ownerID)},
		{Text: "⏭ Следующий", Payload: fmt.Sprintf("next:%d:%d", videoID, ownerID)},
		{Text: "🚨 Пожаловаться", Payload: fmt.Sprintf("report:%d:%d", videoID, ownerID)},
	}}
}

func reportButtons(videoID, ownerID string) [][]maxapi.Button {
	reasons := []string{"Спам", "18+", "Оскорбления", "Мошенничество", "Другое"}
	row := make([]maxapi.Button, 0, len(reasons))
	for _, reason := range reasons {
		row = append(row, maxapi.Button{Text: reason, Payload: fmt.Sprintf("report_reason:%s:%s:%s", videoID, ownerID, reason)})
	}
	return [][]maxapi.Button{row}
}

func mainMenuButtons() [][]maxapi.Button {
	return [][]maxapi.Button{{
		{Text: "📬 Взаимные лайки", Payload: "matches"},
		{Text: "🎥 Перезаписать видео", Payload: "rewrite_video"},
	}, {
		{Text: "✏️ Поменять данные анкеты", Payload: "edit_profile"},
		{Text: "💎 Управление подпиской", Payload: "premium"},
		{Text: "🚨 Пожаловаться", Payload: "menu_report"},
	}}
}
