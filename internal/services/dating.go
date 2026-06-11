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
	publicBaseURL string
}

func NewDatingService(repo *repositories.Repository, max *maxapi.Client, adminIDs []string, publicBaseURL string) *DatingService {
	return &DatingService{repo: repo, max: max, adminIDs: adminIDs, publicBaseURL: strings.TrimRight(publicBaseURL, "/")}
}

func (s *DatingService) HandleMessage(ctx context.Context, msg maxapi.MessageUpdate) error {
	user, err := s.repo.UpsertPlatformUser(ctx, models.User{
		PlatformUserID:   msg.From.ID,
		PlatformChatID:   msg.Chat.ID,
		PlatformDialogID: msg.Dialog.ID,
		ProfileLink:      msg.From.ProfileLink,
		Username:         msg.From.Username,
	})
	if err != nil {
		return err
	}

	text := strings.TrimSpace(msg.Text)
	switch {
	case text == "/start":
		return s.Start(ctx, *user)
	case text == "/commands" || text == "/help":
		return s.SendCommands(ctx, *user)
	case text == "/browse":
		return s.SendNextCandidate(ctx, *user)
	case text == "/matches":
		return s.SendMatches(ctx, *user)
	case text == "/profile":
		return s.max.SendText(ctx, user.PlatformChatID, "Что хотите изменить?", editProfileButtons())
	case text == "/subscription":
		return s.SendPremiumOffer(ctx, *user)
	case strings.HasPrefix(text, "/link "):
		return s.SaveProfileLink(ctx, *user, strings.TrimSpace(strings.TrimPrefix(text, "/link ")))
	case text == "/admin" && s.isAdmin(*user):
		return s.SendAdminPanel(ctx, *user)
	case text == "/botstats" && s.isAdmin(*user):
		return s.SendStats(ctx, *user)
	case text == "/admin_list" && s.isAdmin(*user):
		return s.max.SendText(ctx, user.PlatformChatID, "Админы: "+strings.Join(s.adminIDs, ", "), nil)
	case text == "/payments" && s.isAdmin(*user):
		return s.max.SendText(ctx, user.PlatformChatID, "Платежи пока не подключены.", nil)
	case text == "/errors" && s.isAdmin(*user):
		return s.max.SendText(ctx, user.PlatformChatID, "Ошибки смотрим в runtime logs Timeweb.", nil)
	case text == "/record":
		if err := s.repo.SetFlowState(ctx, user.ID, models.StateAwaitingRewriteVideo); err != nil {
			return err
		}
		return s.SendRecordPrompt(ctx, *user, "Запишите новый кружок на странице записи.")
	case text == "/tester_reset_me":
		return s.ResetMe(ctx, *user)
	case text == "/admin_reset_store confirm" && s.isAdmin(*user):
		return s.AdminResetStore(ctx, *user)
	case strings.HasPrefix(text, "/user ") && s.isAdmin(*user):
		return s.SendUserCard(ctx, *user, strings.TrimSpace(strings.TrimPrefix(text, "/user ")))
	case strings.HasPrefix(text, "📬 Взаимные лайки"):
		return s.SendMatches(ctx, *user)
	case text == "▶️ Начать просмотр":
		return s.SendNextCandidate(ctx, *user)
	case len(msg.Media) > 0:
		return s.HandleMedia(ctx, *user, msg.Media[0])
	case user.FlowState == models.StateAwaitingName:
		return s.SaveNameStep(ctx, *user, text)
	case user.FlowState == models.StateAwaitingEditName:
		return s.SaveEditedName(ctx, *user, text)
	case user.FlowState == models.StateAwaitingProfileLink:
		return s.SaveProfileLink(ctx, *user, text)
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
	if cb.Chat.ID == "" {
		cb.Chat.ID = user.PlatformChatID
	}
	if cb.Dialog.ID != "" && cb.Dialog.ID != user.PlatformDialogID {
		if err := s.repo.UpdatePlatformDialogID(ctx, user.ID, cb.Dialog.ID); err != nil {
			return err
		}
		user.PlatformDialogID = cb.Dialog.ID
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
	case "save_video":
		if len(parts) == 2 {
			return s.SaveRecordedVideo(ctx, *user, parseID(parts[1]))
		}
	case "rewrite_video":
		if err := s.repo.SetFlowState(ctx, user.ID, models.StateAwaitingRewriteVideo); err != nil {
			return err
		}
		return s.SendRecordPrompt(ctx, *user, "Запишите новый кружок на странице записи. Старое видео станет неактивным.")
	case "edit_profile":
		return s.max.SendText(ctx, cb.Chat.ID, "Что хотите изменить?", editProfileButtons())
	case "edit_profile_menu":
		return s.max.SendText(ctx, cb.Chat.ID, "Что хотите изменить?", editProfileButtons())
	case "edit_data":
		return s.max.SendText(ctx, cb.Chat.ID, "Какие данные изменить?", editDataButtons())
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
	case "edit_profile_link":
		if err := s.repo.SetFlowState(ctx, user.ID, models.StateAwaitingProfileLink); err != nil {
			return err
		}
		return s.max.SendText(ctx, cb.Chat.ID, "Поделитесь своим контактом, чтобы другие пользователи могли написать вам после взаимного лайка.\n\nОтправьте ссылку MAX вида:\nhttps://max.ru/u/...\n\nЕё можно получить через «Поделиться» в своём профиле.", nil)
	case "main_menu":
		return s.max.SendText(ctx, cb.Chat.ID, "Главное меню:", mainMenuButtons())
	case "premium":
		return s.SendPremiumOffer(ctx, *user)
	case "missing_profile_link":
		return s.max.SendText(ctx, cb.Chat.ID, "У этого пользователя пока не добавлена ссылка MAX для личных сообщений. Попросите его отправить боту команду:\n/link https://max.ru/u/...", mainMenuButtons())
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

func (s *DatingService) SendCommands(ctx context.Context, user models.User) error {
	text := strings.Join([]string{
		"Команды бота знакомств:",
		"/start - открыть главное меню",
		"/browse - начать просмотр анкет",
		"/matches - взаимные лайки",
		"/profile - изменить анкету",
		"/subscription - подписка",
		"/link ссылка - сохранить ссылку MAX",
		"/help - помощь",
	}, "\n")
	return s.max.SendText(ctx, user.PlatformChatID, text, mainMenuButtons())
}

func (s *DatingService) SendPremiumOffer(ctx context.Context, user models.User) error {
	if user.IsPremium {
		return s.max.SendText(ctx, user.PlatformChatID, "💎 Premium уже активен.\n\nВы можете писать первым без взаимного лайка и смотреть кружки без ограничений.", mainMenuButtons())
	}
	offerURL := s.publicBaseURL + "/offer"
	text := "💎 Premium доступ\n\n" +
		"Что входит:\n" +
		"• возможность писать первым без взаимного лайка;\n" +
		"• неограниченный просмотр кружков.\n\n" +
		"Нажимая кнопку оплаты, вы соглашаетесь с условиями оферты:\n" + offerURL
	return s.max.SendText(ctx, user.PlatformChatID, text, [][]maxapi.Button{
		{{Text: "💎 Оплатить Premium доступ", URL: s.premiumPaymentURL(user)}},
		{{Text: "▶️ Продолжить просмотр", Payload: "browse"}},
		{{Text: "📄 Оферта", URL: offerURL}},
		{{Text: "☰ Главное меню", Payload: "main_menu"}},
	})
}

func (s *DatingService) SendRecordPrompt(ctx context.Context, user models.User, text string) error {
	return s.max.SendText(ctx, user.PlatformChatID, text+"\n\nОткройте запись в браузере, разрешите камеру и удерживайте красную кнопку.", s.recordButtons(user))
}

func (s *DatingService) ResetMe(ctx context.Context, user models.User) error {
	if err := s.repo.ResetUser(ctx, user.ID); err != nil {
		return err
	}
	if err := s.repo.SetFlowState(ctx, user.ID, models.StateAwaitingName); err != nil {
		return err
	}
	return s.max.SendText(ctx, user.PlatformChatID, "Ваш профиль очищен. Заполним анкету заново: отправьте имя от 2 до 30 символов.", nil)
}

func (s *DatingService) AdminResetStore(ctx context.Context, user models.User) error {
	if err := s.repo.ClearAllData(ctx); err != nil {
		return err
	}
	return s.max.SendText(ctx, user.PlatformChatID, "База полностью очищена.", nil)
}

func (s *DatingService) SaveRecordedVideo(ctx context.Context, user models.User, videoID int64) error {
	if videoID == 0 {
		return nil
	}
	if err := s.repo.ActivateVideo(ctx, user.ID, videoID); err != nil {
		if err == repositories.ErrNotFound {
			return s.SendRecordPrompt(ctx, user, "Не нашел этот кружок. Запишите заново.")
		}
		return err
	}
	if err := s.repo.ClearFlowState(ctx, user.ID); err != nil {
		return err
	}
	return s.max.SendText(ctx, user.PlatformChatID, "✅ Кружок успешно сохранен.", [][]maxapi.Button{
		{{Text: "▶️ Начать просмотр", Payload: "browse"}},
	})
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

func (s *DatingService) SaveProfileLink(ctx context.Context, user models.User, link string) error {
	link = normalizeProfileURL(link)
	if !validProfileLink(link) {
		return s.max.SendText(ctx, user.PlatformChatID, "Отправьте ссылку MAX в формате:\nhttps://max.ru/u/...", nil)
	}
	if err := s.repo.UpdateProfileLink(ctx, user.ID, link); err != nil {
		return err
	}
	if err := s.repo.ClearFlowState(ctx, user.ID); err != nil {
		return err
	}
	return s.max.SendText(ctx, user.PlatformChatID, "Ссылка MAX сохранена. Теперь при взаимном лайке или Premium-контакте кнопка «Написать» будет открывать личные сообщения.", mainMenuButtons())
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
	return s.SendRecordPrompt(ctx, user, "Запишите короткий кружок до 30 секунд.")
}

func (s *DatingService) HandleMedia(ctx context.Context, user models.User, media maxapi.Media) error {
	expectingVideo := user.FlowState == models.StateAwaitingVideo || user.FlowState == models.StateAwaitingRewriteVideo
	if !expectingVideo && !profileComplete(user) {
		return s.max.SendText(ctx, user.PlatformChatID, "Чтобы заменить видео, нажмите 🎥 Перезаписать видео.", mainMenuButtons())
	}
	if media.Type != "video" && media.Type != "round_video" && media.Type != "file" {
		return s.max.SendText(ctx, user.PlatformChatID, "Принимается только поддерживаемое короткое видео MAX.", nil)
	}
	if media.Duration > 30 {
		return s.max.SendText(ctx, user.PlatformChatID, "Видео должно быть не длиннее 30 секунд.", nil)
	}
	if err := s.repo.SaveVideo(ctx, user.ID, media.ID, media.URL, media.Duration); err != nil {
		return err
	}
	if err := s.repo.ClearFlowState(ctx, user.ID); err != nil {
		return err
	}
	if user.FlowState == models.StateAwaitingRewriteVideo || !expectingVideo {
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
			return s.SendRecordPrompt(ctx, user, "Сначала запишите свой кружок.")
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
	_, err = s.max.SendMediaToDialogOrUser(ctx, user.PlatformDialogID, user.PlatformChatID, candidate.PlatformMediaID, candidate.Owner.Name, browseButtons(candidate.ID, candidate.Owner.ID))
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
		owner, err := s.repo.GetUserByID(ctx, ownerID)
		if err != nil {
			return err
		}
		reverse, err := s.repo.HasReverseLike(ctx, user.ID, ownerID)
		if err != nil {
			return err
		}
		if !reverse && !user.IsPremium {
			return s.SendPremiumOffer(ctx, user)
		}
		if _, err := s.repo.CreateLike(ctx, user.ID, ownerID); err != nil {
			return err
		}
		if err := s.repo.EnqueuePriority(ctx, ownerID, user.ID); err != nil {
			return err
		}
		if reverse {
			if err := s.repo.CreateMatch(ctx, user.ID, ownerID); err != nil {
				return err
			}
			if err := s.max.SendText(ctx, owner.PlatformChatID, "❤️ У вас новый взаимный лайк!\n\n"+contactLine(user), contactButtons(user, true)); err != nil {
				return err
			}
			return s.max.SendText(ctx, chatID, "❤️ У вас новый взаимный лайк!\n\n"+contactLine(*owner), contactButtons(*owner, true))
		}
		return s.max.SendText(ctx, chatID, "💎 Premium: контакт открыт без взаимного лайка.\n\n"+contactLine(*owner), contactButtons(*owner, false))
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
	messageID, err := s.max.SendMediaToDialogOrUser(ctx, user.PlatformDialogID, user.PlatformChatID, video.PlatformMediaID, "Видео контакта", nil)
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
	text := strings.Join([]string{
		"Админ-меню:",
		"/botstats - общая статистика",
		"/user id - карточка пользователя по ID",
		"/tester_reset_me - очистить свой профиль",
		"/admin_reset_store confirm - полностью очистить базу бота",
		"",
		"Эта команда скрыта из общего списка. Публичные команды доступны через /help.",
	}, "\n")
	return s.max.SendText(ctx, user.PlatformChatID, text, [][]maxapi.Button{
		{{Text: "📊 Статистика", Payload: "admin:stats"}},
		{{Text: "👥 Пользователи", Payload: "admin:users"}},
		{{Text: "🧹 Очистить мой профиль", Payload: "admin:reset_me"}},
		{{Text: "🗑 Очистить базу", Payload: "admin:reset_store_prompt"}},
		{{Text: "☰ Главное меню", Payload: "main_menu"}},
	})
}

func (s *DatingService) SendStats(ctx context.Context, user models.User) error {
	stats, err := s.repo.Stats(ctx)
	if err != nil {
		return err
	}
	return s.max.SendText(ctx, user.PlatformChatID, fmt.Sprintf(
		"📊 Статистика\nВсего пользователей: %d\nАктивных: %d\nВидео: %d\nЛайков: %d\nMatches: %d\nЖалоб: %d\nPremium: %d",
		stats["users"], stats["active_users"], stats["videos"], stats["likes"], stats["matches"], stats["reports"], stats["premium_users"],
	), nil)
}

func (s *DatingService) SendUserCard(ctx context.Context, admin models.User, idText string) error {
	target, err := s.repo.GetUserByID(ctx, parseID(idText))
	if err != nil {
		if err == repositories.ErrNotFound {
			return s.max.SendText(ctx, admin.PlatformChatID, "Пользователь не найден.", nil)
		}
		return err
	}
	videoStatus := "нет"
	if _, err := s.repo.GetActiveVideoByUser(ctx, target.ID); err == nil {
		videoStatus = "есть"
	}
	text := fmt.Sprintf(
		"Карточка пользователя #%d\nplatform_user_id: %s\nchat_id: %s\ndialog_id: %s\nname: %s\nusername: %s\ngender: %s\npreferred: %s\nstatus: %s\nflow_state: %s\nactive_video: %s",
		target.ID, target.PlatformUserID, target.PlatformChatID, target.PlatformDialogID, target.Name, target.Username, target.Gender, target.PreferredGender, target.Status, target.FlowState, videoStatus,
	)
	return s.max.SendText(ctx, admin.PlatformChatID, text, [][]maxapi.Button{{
		{Text: "🚫 Заблокировать", Payload: fmt.Sprintf("admin:block:%d", target.ID)},
		{Text: "✅ Разблокировать", Payload: fmt.Sprintf("admin:unblock:%d", target.ID)},
		{Text: "🗑 Удалить видео", Payload: fmt.Sprintf("admin:delete_video:%d", target.ID)},
	}})
}

func (s *DatingService) HandleAdmin(ctx context.Context, user models.User, parts []string) error {
	if len(parts) < 2 {
		return s.SendAdminPanel(ctx, user)
	}
	switch parts[1] {
	case "stats":
		return s.SendStats(ctx, user)
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
	case "reset_me":
		return s.ResetMe(ctx, user)
	case "reset_store_prompt":
		return s.max.SendText(ctx, user.PlatformChatID, "Для полной очистки базы отправьте текстом:\n/admin_reset_store confirm", nil)
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
	return "Контакт: " + displayName(user)
}

func profileURL(user models.User) string {
	if user.ProfileLink != "" {
		return normalizeProfileURL(user.ProfileLink)
	}
	return ""
}

func normalizeProfileURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	if strings.HasPrefix(value, "u/") {
		return "https://max.ru/" + value
	}
	if strings.HasPrefix(value, "@") {
		return "https://max.ru/" + strings.TrimPrefix(value, "@")
	}
	return value
}

func validProfileLink(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "https://max.ru/u/") || strings.HasPrefix(value, "http://max.ru/u/")
}

func contactButtons(user models.User, includeMatches bool) [][]maxapi.Button {
	buttons := [][]maxapi.Button{}
	if url := profileURL(user); url != "" {
		buttons = append(buttons, []maxapi.Button{{Text: "💬 Написать " + shortName(user.Name), URL: url}})
	} else {
		buttons = append(buttons, []maxapi.Button{{Text: "💬 Ссылка профиля недоступна", Payload: "missing_profile_link"}})
	}
	if includeMatches {
		buttons = append(buttons, []maxapi.Button{{Text: "📬 Взаимные лайки", Payload: "matches"}})
	}
	buttons = append(buttons, []maxapi.Button{{Text: "▶️ Продолжить просмотр", Payload: "browse"}})
	return buttons
}

func genderButtons() [][]maxapi.Button {
	return [][]maxapi.Button{{{Text: "Мужской", Payload: "gender:male"}, {Text: "Женский", Payload: "gender:female"}}}
}

func preferredButtons() [][]maxapi.Button {
	return [][]maxapi.Button{{{Text: "Мужские", Payload: "preferred:male"}, {Text: "Женские", Payload: "preferred:female"}, {Text: "Не важно", Payload: "preferred:any"}}}
}

func browseButtons(videoID, ownerID int64) [][]maxapi.Button {
	return [][]maxapi.Button{
		{{Text: "❤️ Написать", Payload: fmt.Sprintf("like:%d:%d", videoID, ownerID)}},
		{{Text: "⏭ Следующий", Payload: fmt.Sprintf("next:%d:%d", videoID, ownerID)}},
		{
			{Text: "🚨 Пожаловаться", Payload: fmt.Sprintf("report:%d:%d", videoID, ownerID)},
			{Text: "☰ Меню", Payload: "main_menu"},
		},
	}
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
	return [][]maxapi.Button{
		{{Text: "🎥 Изменить видео", Payload: "rewrite_video"}},
		{{Text: "✏️ Изменить данные", Payload: "edit_data"}},
		{{Text: "☰ Главное меню", Payload: "main_menu"}},
	}
}

func editDataButtons() [][]maxapi.Button {
	return [][]maxapi.Button{
		{{Text: "Имя", Payload: "edit_name"}},
		{{Text: "Пол", Payload: "edit_gender"}},
		{{Text: "Кого смотреть", Payload: "edit_preferred"}},
		{{Text: "💬 Поделиться своим контактом", Payload: "edit_profile_link"}},
		{{Text: "☰ Главное меню", Payload: "main_menu"}},
	}
}

func (s *DatingService) recordButtons(user models.User) [][]maxapi.Button {
	return [][]maxapi.Button{{{Text: "🎥 Открыть запись", URL: s.recordURL(user)}}}
}

func (s *DatingService) recordURL(user models.User) string {
	return s.publicBaseURL + "/mini/record?u=" + user.PlatformUserID
}

func (s *DatingService) premiumPaymentURL(user models.User) string {
	return s.publicBaseURL + "/pay?u=" + user.PlatformUserID
}

func mainMenuButtons() [][]maxapi.Button {
	return [][]maxapi.Button{
		{{Text: "▶️ Начать просмотр", Payload: "browse"}},
		{{Text: "📬 Взаимные лайки", Payload: "matches"}},
		{{Text: "✏️ Изменить анкету", Payload: "edit_profile"}},
		{
			{Text: "🚨 Пожаловаться", Payload: "menu_report"},
			{Text: "💎 Подписка", Payload: "premium"},
		},
	}
}
