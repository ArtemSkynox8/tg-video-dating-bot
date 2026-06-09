package services

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/maxapi"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/models"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/repositories"
)

type DatingService struct {
	repo     *repositories.Repository
	max      *maxapi.Client
	adminIDs []string
}

func NewDatingService(repo *repositories.Repository, max *maxapi.Client, adminIDs []string) *DatingService {
	return &DatingService{repo: repo, max: max, adminIDs: adminIDs}
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
		return s.Start(ctx, *user)
	case text == "/admin" && s.isAdmin(*user):
		return s.SendAdminPanel(ctx, *user)
	case strings.HasPrefix(text, "📬 Взаимные лайки"):
		return s.SendMatches(ctx, *user)
	case text == "▶️ Начать просмотр" || text == "/browse":
		return s.SendNextCandidate(ctx, *user)
	case len(msg.Media) > 0:
		return s.HandleMedia(ctx, *user, msg.Media[0])
	case user.FlowState == models.StateAwaitingName:
		return s.SaveNameStep(ctx, *user, text)
	case user.FlowState == models.StateAwaitingEditName:
		return s.SaveEditedName(ctx, *user, text)
	case user.Name == "":
		return s.SaveNameStep(ctx, *user, text)
	default:
		return s.max.SendText(ctx, user.PlatformChatID, "Выберите действие в меню.", mainMenuButtons())
	}
}

func (s *DatingService) HandleCallback(ctx context.Context, cb maxapi.CallbackUpdate) error {
	user, err := s.repo.GetUserByPlatformID(ctx, cb.From.ID)
	if err != nil {
		return err
	}
	parts := strings.Split(cb.Payload, ":")
	if len(parts) == 0 {
		return nil
	}

	switch parts[0] {
	case "browse":
		return s.SendNextCandidate(ctx, *user)
	case "gender":
		if len(parts) == 2 {
			return s.SaveGenderStep(ctx, *user, parts[1])
		}
	case "preferred":
		if len(parts) == 2 {
			return s.SavePreferredGenderStep(ctx, *user, parts[1])
		}
	case "like", "next":
		if len(parts) == 3 {
			action := models.ActionNext
			if parts[0] == "like" {
				action = models.ActionLike
			}
			return s.HandleBrowseAction(ctx, *user, cb.Chat.ID, cb.MessageID, parts[1], parts[2], action)
		}
	case "report":
		if len(parts) == 3 {
			return s.max.SendText(ctx, cb.Chat.ID, "Выберите причину жалобы:", reportButtons(parts[1], parts[2]))
		}
	case "report_reason":
		if len(parts) == 4 {
			return s.HandleVideoReport(ctx, *user, cb.Chat.ID, cb.MessageID, parts[1], parts[2], parts[3])
		}
	case "matches":
		return s.SendMatches(ctx, *user)
	case "match_video":
		if len(parts) == 2 {
			return s.SendMatchVideo(ctx, *user, parseID(parts[1]))
		}
	case "hide_match":
		if len(parts) == 2 {
			return s.HideMatch(ctx, *user, parseID(parts[1]))
		}
	case "rewrite_video":
		if err := s.repo.SetFlowState(ctx, user.ID, models.StateAwaitingRewriteVideo); err != nil {
			return err
		}
		return s.max.SendText(ctx, cb.Chat.ID, "Отправьте новое короткое видео до 60 секунд. Старое видео станет неактивным.", nil)
	case "edit_profile":
		return s.max.SendText(ctx, cb.Chat.ID, "Что изменить?", editProfileButtons())
	case "edit_name":
		if err := s.repo.SetFlowState(ctx, user.ID, models.StateAwaitingEditName); err != nil {
			return err
		}
		return s.max.SendText(ctx, cb.Chat.ID, "Отправьте новое имя от 2 до 30 символов.", nil)
	case "edit_gender":
		if err := s.repo.SetFlowState(ctx, user.ID, models.StateAwaitingGender); err != nil {
			return err
		}
		return s.max.SendText(ctx, cb.Chat.ID, "Выберите свой пол:", genderButtons())
	case "edit_preferred":
		if err := s.repo.SetFlowState(ctx, user.ID, models.StateAwaitingPreferredGender); err != nil {
			return err
		}
		return s.max.SendText(ctx, cb.Chat.ID, "Какие видео хотите получать?", preferredButtons())
	case "premium":
		return s.max.SendText(ctx, cb.Chat.ID, "💎 Premium подготовлен как отдельный модуль. Платежи подключим после проверки платежных возможностей MAX.", mainMenuButtons())
	case "menu_report":
		return s.SendReportableMatches(ctx, *user)
	case "report_user":
		if len(parts) == 2 {
			return s.max.SendText(ctx, cb.Chat.ID, "Выберите причину жалобы:", userReportButtons(parts[1]))
		}
	case "user_report_reason":
		if len(parts) == 3 {
			return s.HandleUserReport(ctx, *user, parseID(parts[1]), parts[2])
		}
	case "admin":
		if s.isAdmin(*user) {
			return s.HandleAdmin(ctx, *user, parts)
		}
	}
	return nil
}

func (s *DatingService) Start(ctx context.Context, user models.User) error {
	if !profileComplete(user) {
		if err := s.repo.SetFlowState(ctx, user.ID, models.StateAwaitingName); err != nil {
			return err
		}
		return s.max.SendText(ctx, user.PlatformChatID, "Привет. Заполним анкету: отправьте имя от 2 до 30 символов.", nil)
	}
	return s.max.SendText(ctx, user.PlatformChatID, "Вы уже зарегистрированы. Выберите действие.", mainMenuButtons())
}

func (s *DatingService) SaveNameStep(ctx context.Context, user models.User, name string) error {
	if !validName(name) {
		return s.max.SendText(ctx, user.PlatformChatID, "Имя должно быть от 2 до 30 символов: буквы, пробелы и дефисы.", nil)
	}
	if err := s.repo.UpdateName(ctx, user.ID, name); err != nil {
		return err
	}
	if err := s.repo.SetFlowState(ctx, user.ID, models.StateAwaitingGender); err != nil {
		return err
	}
	return s.max.SendText(ctx, user.PlatformChatID, "Выберите свой пол:", genderButtons())
}

func (s *DatingService) SaveEditedName(ctx context.Context, user models.User, name string) error {
	if !validName(name) {
		return s.max.SendText(ctx, user.PlatformChatID, "Имя должно быть от 2 до 30 символов: буквы, пробелы и дефисы.", nil)
	}
	if err := s.repo.UpdateName(ctx, user.ID, name); err != nil {
		return err
	}
	if err := s.repo.ClearFlowState(ctx, user.ID); err != nil {
		return err
	}
	return s.max.SendText(ctx, user.PlatformChatID, "Имя обновлено.", mainMenuButtons())
}

func (s *DatingService) SaveGenderStep(ctx context.Context, user models.User, gender string) error {
	gender = normalizeGender(gender)
	if gender == "" {
		return s.max.SendText(ctx, user.PlatformChatID, "Выберите: Мужской или Женский.", genderButtons())
	}
	if err := s.repo.UpdateGender(ctx, user.ID, gender); err != nil {
		return err
	}
	if user.FlowState == models.StateAwaitingGender && user.PreferredGender != "" {
		if err := s.repo.ClearFlowState(ctx, user.ID); err != nil {
			return err
		}
		return s.max.SendText(ctx, user.PlatformChatID, "Пол обновлен.", mainMenuButtons())
	}
	if err := s.repo.SetFlowState(ctx, user.ID, models.StateAwaitingPreferredGender); err != nil {
		return err
	}
	return s.max.SendText(ctx, user.PlatformChatID, "Какие видео хотите получать?", preferredButtons())
}

func (s *DatingService) SavePreferredGenderStep(ctx context.Context, user models.User, preferred string) error {
	preferred = normalizePreferredGender(preferred)
	if preferred == "" {
		return s.max.SendText(ctx, user.PlatformChatID, "Выберите: Мужские, Женские или Не важно.", preferredButtons())
	}
	if err := s.repo.UpdatePreferredGender(ctx, user.ID, preferred); err != nil {
		return err
	}
	if user.FlowState == models.StateAwaitingPreferredGender && user.Name != "" && user.Gender != "" {
		if _, err := s.repo.GetActiveVideoByUser(ctx, user.ID); err == nil {
			if err := s.repo.ClearFlowState(ctx, user.ID); err != nil {
				return err
			}
			return s.max.SendText(ctx, user.PlatformChatID, "Предпочтения обновлены.", mainMenuButtons())
		}
	}
	if err := s.repo.SetFlowState(ctx, user.ID, models.StateAwaitingVideo); err != nil {
		return err
	}
	return s.max.SendText(ctx, user.PlatformChatID, "Отправьте короткое видео до 60 секунд.", nil)
}

func (s *DatingService) HandleMedia(ctx context.Context, user models.User, media maxapi.Media) error {
	if user.FlowState != models.StateAwaitingVideo && user.FlowState != models.StateAwaitingRewriteVideo {
		return s.max.SendText(ctx, user.PlatformChatID, "Чтобы заменить видео, нажмите 🎥 Перезаписать видео.", mainMenuButtons())
	}
	if media.Type != "video" && media.Type != "round_video" && media.Type != "file" {
		return s.max.SendText(ctx, user.PlatformChatID, "Принимается только поддерживаемое короткое видео MAX.", nil)
	}
	if media.Duration > 60 {
		return s.max.SendText(ctx, user.PlatformChatID, "Видео должно быть не длиннее 60 секунд.", nil)
	}
	if err := s.repo.SaveVideo(ctx, user.ID, media.ID, media.URL, media.Duration); err != nil {
		return err
	}
	if err := s.repo.ClearFlowState(ctx, user.ID); err != nil {
		return err
	}
	if user.FlowState == models.StateAwaitingRewriteVideo {
		return s.max.SendText(ctx, user.PlatformChatID, "Видео обновлено.", mainMenuButtons())
	}
	return s.max.SendText(ctx, user.PlatformChatID, "✅ Анкета создана. Теперь вы можете смотреть видео других пользователей.", [][]maxapi.Button{
		{{Text: "▶️ Начать просмотр", Payload: "browse"}},
	})
}

func (s *DatingService) SendNextCandidate(ctx context.Context, user models.User) error {
	if !profileComplete(user) {
		return s.Start(ctx, user)
	}
	if user.Status == models.StatusBlocked {
		return s.max.SendText(ctx, user.PlatformChatID, "Ваш профиль заблокирован.", nil)
	}
	if user.RestrictedUntil != nil && user.RestrictedUntil.After(time.Now()) {
		return s.max.SendText(ctx, user.PlatformChatID, "Просмотр новых видео временно ограничен до "+user.RestrictedUntil.Format("02.01.2006 15:04")+".", mainMenuButtons())
	}
	if _, err := s.repo.GetActiveVideoByUser(ctx, user.ID); err != nil {
		if err == repositories.ErrNotFound {
			if setErr := s.repo.SetFlowState(ctx, user.ID, models.StateAwaitingVideo); setErr != nil {
				return setErr
			}
			return s.max.SendText(ctx, user.PlatformChatID, "Сначала отправьте свое короткое видео.", nil)
		}
		return err
	}
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

func (s *DatingService) HandleVideoReport(ctx context.Context, user models.User, chatID, messageID, videoIDText, ownerIDText, reason string) error {
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
	buttons := [][]maxapi.Button{}
	for _, u := range users {
		lines = append(lines, contactLine(u))
		buttons = append(buttons, []maxapi.Button{
			{Text: "💬 " + shortName(u.Name), URL: profileURL(u)},
			{Text: "🎥 Видео", Payload: fmt.Sprintf("match_video:%d", u.ID)},
			{Text: "🗑 Удалить", Payload: fmt.Sprintf("hide_match:%d", u.ID)},
		})
	}
	return s.max.SendText(ctx, user.PlatformChatID, strings.Join(lines, "\n"), buttons)
}

func (s *DatingService) SendMatchVideo(ctx context.Context, user models.User, otherUserID int64) error {
	if otherUserID == 0 {
		return nil
	}
	if _, err := s.repo.FindVisibleMatch(ctx, user.ID, otherUserID); err != nil {
		return s.max.SendText(ctx, user.PlatformChatID, "Этот контакт недоступен.", mainMenuButtons())
	}
	video, err := s.repo.GetActiveVideoByUser(ctx, otherUserID)
	if err != nil {
		if err == repositories.ErrNotFound {
			return s.max.SendText(ctx, user.PlatformChatID, "У контакта нет активного видео.", mainMenuButtons())
		}
		return err
	}
	messageID, err := s.max.SendMedia(ctx, user.PlatformChatID, video.PlatformMediaID, "Видео контакта", nil)
	if err != nil {
		return err
	}
	go func(chatID, mid string) {
		time.Sleep(60 * time.Second)
		_ = s.max.DeleteMessage(context.Background(), chatID, mid)
	}(user.PlatformChatID, messageID)
	return nil
}

func (s *DatingService) HideMatch(ctx context.Context, user models.User, otherUserID int64) error {
	if otherUserID == 0 {
		return nil
	}
	if err := s.repo.HideMatchForUser(ctx, user.ID, otherUserID); err != nil {
		return err
	}
	return s.max.SendText(ctx, user.PlatformChatID, "Контакт скрыт из вашего списка.", mainMenuButtons())
}

func (s *DatingService) SendReportableMatches(ctx context.Context, user models.User) error {
	users, err := s.repo.ListVisibleMatches(ctx, user.ID)
	if err != nil {
		return err
	}
	if len(users) == 0 {
		return s.max.SendText(ctx, user.PlatformChatID, "Жалобы из меню доступны только на пользователей из взаимных лайков.", mainMenuButtons())
	}
	buttons := [][]maxapi.Button{}
	for _, u := range users {
		buttons = append(buttons, []maxapi.Button{{Text: u.Name, Payload: fmt.Sprintf("report_user:%d", u.ID)}})
	}
	return s.max.SendText(ctx, user.PlatformChatID, "Выберите пользователя из взаимных лайков:", buttons)
}

func (s *DatingService) HandleUserReport(ctx context.Context, user models.User, reportedUserID int64, reason string) error {
	matchID, err := s.repo.FindVisibleMatch(ctx, user.ID, reportedUserID)
	if err != nil {
		if err == repositories.ErrNotFound {
			return s.max.SendText(ctx, user.PlatformChatID, "Жаловаться можно только на пользователей из взаимных лайков.", mainMenuButtons())
		}
		return err
	}
	if err := s.repo.CreateUserReport(ctx, user.ID, reportedUserID, matchID, reason); err != nil {
		return err
	}
	if err := s.repo.ApplyUserReportRestrictions(ctx, reportedUserID); err != nil {
		return err
	}
	return s.max.SendText(ctx, user.PlatformChatID, "Жалоба сохранена.", mainMenuButtons())
}

func (s *DatingService) SendAdminPanel(ctx context.Context, user models.User) error {
	return s.max.SendText(ctx, user.PlatformChatID, "Админ-панель:", [][]maxapi.Button{
		{{Text: "📊 Статистика", Payload: "admin:stats"}, {Text: "👥 Пользователи", Payload: "admin:users"}},
	})
}

func (s *DatingService) HandleAdmin(ctx context.Context, user models.User, parts []string) error {
	if len(parts) < 2 {
		return s.SendAdminPanel(ctx, user)
	}
	switch parts[1] {
	case "stats":
		stats, err := s.repo.Stats(ctx)
		if err != nil {
			return err
		}
		return s.max.SendText(ctx, user.PlatformChatID, fmt.Sprintf(
			"📊 Статистика\nВсего пользователей: %d\nАктивных: %d\nВидео: %d\nЛайков: %d\nMatches: %d\nЖалоб: %d\nPremium: %d",
			stats["users"], stats["active_users"], stats["videos"], stats["likes"], stats["matches"], stats["reports"], stats["premium_users"],
		), nil)
	case "users":
		users, err := s.repo.ListUsers(ctx, 20)
		if err != nil {
			return err
		}
		lines := []string{"👥 Последние пользователи:"}
		buttons := [][]maxapi.Button{}
		for _, u := range users {
			lines = append(lines, fmt.Sprintf("#%d %s status=%s", u.ID, displayName(u), u.Status))
			buttons = append(buttons, []maxapi.Button{
				{Text: "🚫 #" + fmt.Sprint(u.ID), Payload: fmt.Sprintf("admin:block:%d", u.ID)},
				{Text: "✅ #" + fmt.Sprint(u.ID), Payload: fmt.Sprintf("admin:unblock:%d", u.ID)},
				{Text: "🗑 Видео #" + fmt.Sprint(u.ID), Payload: fmt.Sprintf("admin:delete_video:%d", u.ID)},
			})
		}
		return s.max.SendText(ctx, user.PlatformChatID, strings.Join(lines, "\n"), buttons)
	case "block":
		if len(parts) == 3 {
			if err := s.repo.SetUserStatus(ctx, parseID(parts[2]), models.StatusBlocked); err != nil {
				return err
			}
			return s.max.SendText(ctx, user.PlatformChatID, "Пользователь заблокирован.", nil)
		}
	case "unblock":
		if len(parts) == 3 {
			if err := s.repo.SetUserStatus(ctx, parseID(parts[2]), models.StatusActive); err != nil {
				return err
			}
			return s.max.SendText(ctx, user.PlatformChatID, "Пользователь разблокирован.", nil)
		}
	case "delete_video":
		if len(parts) == 3 {
			if err := s.repo.DeleteActiveVideo(ctx, parseID(parts[2])); err != nil {
				return err
			}
			return s.max.SendText(ctx, user.PlatformChatID, "Активное видео удалено.", nil)
		}
	}
	return nil
}

func (s *DatingService) isAdmin(user models.User) bool {
	return slices.Contains(s.adminIDs, user.PlatformUserID)
}

func profileComplete(user models.User) bool {
	return user.Name != "" && user.Gender != "" && user.PreferredGender != ""
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

func displayName(user models.User) string {
	if user.Name != "" {
		return user.Name
	}
	if user.Username != "" {
		return user.Username
	}
	return user.PlatformUserID
}

func shortName(name string) string {
	if name == "" {
		return "Написать"
	}
	return name
}

func contactLine(user models.User) string {
	return fmt.Sprintf("%s | 💬 Написать | 🎥 Смотреть видео | 🗑 Удалить из списка", displayName(user))
}

func profileURL(user models.User) string {
	if user.ProfileLink != "" {
		return user.ProfileLink
	}
	return "max://user/" + user.PlatformUserID
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

func userReportButtons(userID string) [][]maxapi.Button {
	reasons := []string{"Спам", "Оскорбления", "Мошенничество", "Нежелательный контент", "Другое"}
	row := make([]maxapi.Button, 0, len(reasons))
	for _, reason := range reasons {
		row = append(row, maxapi.Button{Text: reason, Payload: fmt.Sprintf("user_report_reason:%s:%s", userID, reason)})
	}
	return [][]maxapi.Button{row}
}

func editProfileButtons() [][]maxapi.Button {
	return [][]maxapi.Button{{
		{Text: "Имя", Payload: "edit_name"},
		{Text: "Пол", Payload: "edit_gender"},
		{Text: "Кого смотреть", Payload: "edit_preferred"},
	}}
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
