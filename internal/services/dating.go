package services

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/maxapi"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/models"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/repositories"
)

type DatingService struct {
	repo                      *repositories.Repository
	max                       *maxapi.Client
	adminIDs                  []string
	publicBaseURL             string
	premiumPrice              string
	contactInstructionVideoID string
	contactInstructionVideoPath  string
	contactInstructionVideoToken string
	contactInstructionVideoMu    sync.Mutex
	forwards                  map[string]maxapi.ForwardInfo
	forwardsMu                sync.RWMutex
}

func NewDatingService(repo *repositories.Repository, max *maxapi.Client, adminIDs []string, publicBaseURL, premiumPrice, contactInstructionVideoID, contactInstructionVideoPath string) *DatingService {
	return &DatingService{repo: repo, max: max, adminIDs: adminIDs, publicBaseURL: strings.TrimRight(publicBaseURL, "/"), premiumPrice: premiumPrice, contactInstructionVideoID: strings.TrimSpace(contactInstructionVideoID), contactInstructionVideoPath: strings.TrimSpace(contactInstructionVideoPath), forwards: map[string]maxapi.ForwardInfo{}}
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
	if msg.Forward != nil {
		s.saveForward(msg.From.ID, *msg.Forward)
		if text == "" {
			return s.max.SendText(ctx, user.PlatformChatID, "Пересланное сообщение сохранено для теста.", mainMenuButtons())
		}
	}
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
		return s.SendSubscriptionStatus(ctx, *user)
	case text == "/premium":
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
	case strings.HasPrefix(text, "/admin_deeplink ") && s.isAdmin(*user):
		return s.SendAdminDeepLinkTest(ctx, *user, strings.TrimSpace(strings.TrimPrefix(text, "/admin_deeplink ")))
	case strings.HasPrefix(text, "/admin_deeplink_text ") && s.isAdmin(*user):
		return s.SendAdminDeepLinkTextTest(ctx, *user, strings.TrimSpace(strings.TrimPrefix(text, "/admin_deeplink_text ")))
	case strings.HasPrefix(text, "/admin_phone_link_text ") && s.isAdmin(*user):
		return s.SendAdminPhoneLinkTextTest(ctx, *user, strings.TrimSpace(strings.TrimPrefix(text, "/admin_phone_link_text ")))
	case strings.HasPrefix(text, "/admin_send_contact ") && s.isAdmin(*user):
		return s.SendAdminContactCardTest(ctx, *user, strings.TrimSpace(strings.TrimPrefix(text, "/admin_send_contact ")))
	case strings.HasPrefix(text, "/admin_send_forward ") && s.isAdmin(*user):
		return s.SendAdminForwardTest(ctx, *user, strings.TrimSpace(strings.TrimPrefix(text, "/admin_send_forward ")))
	case strings.HasPrefix(text, "/user ") && s.isAdmin(*user):
		return s.SendUserCard(ctx, *user, strings.TrimSpace(strings.TrimPrefix(text, "/user ")))
	case strings.HasPrefix(text, "📬 Взаимные лайки"):
		return s.SendMatches(ctx, *user)
	case text == "▶️ Начать просмотр":
		return s.SendNextCandidate(ctx, *user)
	case len(msg.Contacts) > 0:
		return s.SaveContactPhone(ctx, *user, msg.Contacts[0])
	case len(msg.Media) > 0:
		return s.HandleMedia(ctx, *user, msg.Media[0])
	case user.FlowState == models.StateAwaitingName:
		return s.SaveNameStep(ctx, *user, text)
	case user.FlowState == models.StateAwaitingEditName:
		return s.SaveEditedName(ctx, *user, text)
	case user.FlowState == models.StateAwaitingProfileLink:
		if text != "" || msg.From.ProfileLink != "" {
			return s.SaveProfileLink(ctx, *user, firstNonEmptyString(msg.From.ProfileLink, text))
		}
		if len(msg.ImageURLs) > 0 {
			return s.SaveProfileLinkFromQR(ctx, *user, msg.ImageURLs)
		}
		return s.SendProfileShareInstructions(ctx, *user)
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
	case "reset_browse":
		return s.ResetBrowse(ctx, *user)
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
	case "like_only":
		if len(parts) == 3 {
			return s.HandleLikeOnly(ctx, *user, cb.Chat.ID, cb.MessageID, parts[1], parts[2])
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
	case "match_actions":
		if len(parts) == 2 {
			return s.SendMatchActions(ctx, *user, parseID(parts[1]))
		}
	case "match_video":
		if len(parts) == 2 {
			return s.SendMatchVideo(ctx, *user, parseID(parts[1]))
		}
	case "match_contact":
		if len(parts) == 2 {
			return s.SendMatchContactCard(ctx, *user, parseID(parts[1]))
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
		return s.SendProfileShareInstructions(ctx, *user)
	case "main_menu":
		return s.max.SendText(ctx, cb.Chat.ID, "Главное меню:", mainMenuButtons())
	case "premium", "subscription":
		return s.SendSubscriptionStatus(ctx, *user)
	case "unsubscribe":
		return s.SendUnsubscribeStub(ctx, *user)
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
		return s.SendSubscriptionStatus(ctx, user)
	}
	offerURL := s.publicBaseURL + "/offer"
	text := "💎 Premium доступ\n\n" +
		"Стоимость: " + s.premiumPriceText() + ".\n\n" +
		"Что входит:\n" +
		"• доступ к контактам пользователей;\n" +
		"• возможность писать первым без взаимного лайка;\n" +
		"• неограниченный просмотр кружков.\n\n" +
		"Нажимая кнопку оплаты, вы соглашаетесь с условиями оферты."
	messageID, err := s.max.SendTextWithID(ctx, user.PlatformChatID, text, [][]maxapi.Button{
		{{Text: "💎 Оплатить Premium доступ", URL: s.premiumPaymentURL(user)}},
		{{Text: "▶️ Продолжить просмотр", Payload: "browse"}},
		{{Text: "📄 Оферта", URL: offerURL}},
		{{Text: "🚫 Отписаться", Payload: "unsubscribe"}},
		{{Text: "☰ Главное меню", Payload: "main_menu"}},
	})
	if err != nil {
		return err
	}
	return s.repo.UpdatePremiumOfferMessage(ctx, user.ID, user.PlatformChatID, messageID)
}

func (s *DatingService) SendSubscriptionStatus(ctx context.Context, user models.User) error {
	if !user.IsPremium {
		return s.SendSubscriptionOffer(ctx, user)
	}
	text := "💎 Подписка\n\n" +
		"Статус: активна.\n\n" +
		"Следующее списание: будет показано после подключения магазина.\n\n" +
		"Premium дает доступ к контактам пользователей, возможность писать первым без взаимного лайка и неограниченный просмотр кружков."
	return s.max.SendText(ctx, user.PlatformChatID, text, subscriptionStatusButtons())
}

func (s *DatingService) SendSubscriptionOffer(ctx context.Context, user models.User) error {
	offerURL := s.publicBaseURL + "/offer"
	text := "💎 Подписка Premium\n\n" +
		"Стоимость: " + s.premiumPriceText() + ".\n\n" +
		"Что входит:\n" +
		"• доступ к контактам пользователей;\n" +
		"• возможность писать первым без взаимного лайка;\n" +
		"• неограниченный просмотр кружков.\n\n" +
		"Нажимая кнопку подписки, вы соглашаетесь с условиями оферты."
	return s.max.SendText(ctx, user.PlatformChatID, text, [][]maxapi.Button{
		{{Text: "💎 Подписаться", URL: s.premiumPaymentURL(user)}},
		{{Text: "📄 Оферта", URL: offerURL}},
		{{Text: "☰ Главное меню", Payload: "main_menu"}},
	})
}

func (s *DatingService) SendUnsubscribeStub(ctx context.Context, user models.User) error {
	return s.max.SendText(ctx, user.PlatformChatID, "Отписка пока работает в ручном режиме. Когда подключим реальный магазин и автосписания, эта кнопка будет отключать продление автоматически.", [][]maxapi.Button{
		{{Text: "💎 Подписка", Payload: "subscription"}},
		{{Text: "☰ Главное меню", Payload: "main_menu"}},
	})
}

func (s *DatingService) SendRecordPrompt(ctx context.Context, user models.User, text string) error {
	return s.max.SendText(ctx, user.PlatformChatID, text+"\n\nОткройте запись в браузере, разрешите камеру и удерживайте красную кнопку.", s.recordButtons(user))
}

func (s *DatingService) SendProfileShareInstructions(ctx context.Context, user models.User) error {
	return s.sendProfileShareInstructions(ctx, user, "Чтобы другие пользователи могли написать вам после взаимного лайка, отправьте боту ссылку на свой профиль MAX.")
}

func (s *DatingService) SendBrowseContactInstructions(ctx context.Context, user models.User) error {
	return s.sendProfileShareInstructions(ctx, user, "Для просмотра кружков поделитесь своим контактом MAX.")
}

func (s *DatingService) sendProfileShareInstructions(ctx context.Context, user models.User, intro string) error {
	text := intro + "\n\n" +
		"Как сделать:\n" +
		"1. Откройте свой профиль MAX.\n" +
		"2. Нажмите «Поделиться».\n" +
		"3. Отправьте профиль в этот чат с ботом.\n\n" +
		"Если на iOS отправляется только QR-код, пришлите эту QR-картинку сюда — бот попробует извлечь ссылку сам."
	if err := s.max.SendText(ctx, user.PlatformChatID, text, contactShareButtons()); err != nil {
		return err
	}
	if err := s.SendContactInstructionVideo(ctx, user); err != nil {
		log.Printf("send contact instruction video user=%s: %v", user.PlatformUserID, err)
	}
	return nil
}

func (s *DatingService) SendContactInstructionVideo(ctx context.Context, user models.User) error {
	token, err := s.ContactInstructionVideoToken(ctx)
	if err != nil || token == "" {
		return err
	}
	_, err = s.max.SendMediaToDialogOrUser(ctx, user.PlatformDialogID, user.PlatformChatID, token, "Короткая видеоинструкция: как поделиться профилем MAX.", nil)
	return err
}

func (s *DatingService) ContactInstructionVideoToken(ctx context.Context) (string, error) {
	if s.contactInstructionVideoID != "" {
		return s.contactInstructionVideoID, nil
	}
	if s.contactInstructionVideoPath == "" {
		return "", nil
	}
	s.contactInstructionVideoMu.Lock()
	defer s.contactInstructionVideoMu.Unlock()
	if s.contactInstructionVideoToken != "" {
		return s.contactInstructionVideoToken, nil
	}
	if _, err := os.Stat(s.contactInstructionVideoPath); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	token, err := s.max.UploadVideo(ctx, s.contactInstructionVideoPath)
	if err != nil {
		return "", err
	}
	s.contactInstructionVideoToken = token
	return token, nil
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
	if strings.TrimSpace(user.ProfileLink) == "" && strings.TrimSpace(user.ContactPhone) == "" {
		return s.max.SendText(ctx, user.PlatformChatID, "✅ Кружок успешно сохранен.\n\nЧтобы другие пользователи могли написать вам после взаимного лайка, добавьте свой контакт MAX.", [][]maxapi.Button{
			{{Text: "📤 Как поделиться профилем MAX", Payload: "edit_profile_link"}},
		})
	}
	return s.max.SendText(ctx, user.PlatformChatID, "✅ Кружок успешно сохранен.", [][]maxapi.Button{{{Text: "▶️ Начать просмотр", Payload: "browse"}}})
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
	link = extractMaxProfileURL(link)
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
	return s.max.SendText(ctx, user.PlatformChatID, "Ссылка MAX сохранена. Теперь при взаимном лайке или Premium-контакте кнопка «Написать» будет открывать личные сообщения.", [][]maxapi.Button{
		{{Text: "▶️ Начать просмотр", Payload: "browse"}},
		{{Text: "☰ Главное меню", Payload: "main_menu"}},
	})
}

func (s *DatingService) SaveProfileLinkFromQR(ctx context.Context, user models.User, imageURLs []string) error {
	for _, imageURL := range imageURLs {
		qrText, err := decodeQRFromImageURL(ctx, imageURL)
		if err != nil {
			continue
		}
		link := extractMaxProfileURL(qrText)
		if validProfileLink(normalizeProfileURL(link)) {
			return s.SaveProfileLink(ctx, user, link)
		}
	}
	return s.max.SendText(ctx, user.PlatformChatID, "Не удалось распознать ссылку профиля в QR-коде. Попробуйте отправить QR крупнее или поделиться профилем через кнопку «Поделиться».", contactShareButtons())
}

func (s *DatingService) SaveContactPhone(ctx context.Context, user models.User, contact maxapi.Contact) error {
	phone := strings.TrimSpace(contact.Phone)
	if phone == "" {
		return s.SendProfileShareInstructions(ctx, user)
	}
	if err := s.repo.UpdateContactPhone(ctx, user.ID, phone); err != nil {
		return err
	}
	if err := s.repo.ClearFlowState(ctx, user.ID); err != nil {
		return err
	}
	return s.max.SendText(ctx, user.PlatformChatID, "Контакт MAX сохранен. Если хотите, можете дополнительно отправить ссылку https://max.ru/u/... - тогда кнопка «Написать» будет открывать личные сообщения напрямую.", [][]maxapi.Button{
		{{Text: "▶️ Начать просмотр", Payload: "browse"}},
		{{Text: "☰ Главное меню", Payload: "main_menu"}},
	})
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
	if !contactComplete(user) {
		if err := s.repo.SetFlowState(ctx, user.ID, models.StateAwaitingProfileLink); err != nil {
			return err
		}
		return s.SendBrowseContactInstructions(ctx, user)
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
	if !contactComplete(user) {
		if err := s.repo.SetFlowState(ctx, user.ID, models.StateAwaitingProfileLink); err != nil {
			return err
		}
		return s.SendBrowseContactInstructions(ctx, user)
	}
	candidate, err := s.repo.FindCandidate(ctx, user.ID)
	if err != nil {
		if err == repositories.ErrNotFound {
			return s.max.SendText(ctx, user.PlatformChatID, "Кружки закончились. Вернитесь попозже или посмотрите кружки заново.", [][]maxapi.Button{
				{{Text: "🔁 Посмотреть заново", Payload: "reset_browse"}},
				{{Text: "☰ Главное меню", Payload: "main_menu"}},
			})
		}
		return err
	}
	_, err = s.max.SendMediaToDialogOrUser(ctx, user.PlatformDialogID, user.PlatformChatID, candidate.PlatformMediaID, candidate.Owner.Name, browseButtons(candidate.ID, candidate.Owner.ID))
	return err
}

func (s *DatingService) ResetBrowse(ctx context.Context, user models.User) error {
	if err := s.repo.ResetBrowseViews(ctx, user.ID); err != nil {
		return err
	}
	return s.SendNextCandidate(ctx, user)
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
			if err := s.sendContactAccess(ctx, owner.PlatformChatID, "❤️ У вас новый взаимный лайк!", user, true); err != nil {
				return err
			}
			return s.sendContactAccess(ctx, chatID, "❤️ У вас новый взаимный лайк!", *owner, true)
		}
		return s.sendContactAccess(ctx, chatID, "💎 Premium: контакт открыт без взаимного лайка.", *owner, false)
	}
	return s.SendNextCandidate(ctx, user)
}

func (s *DatingService) HandleLikeOnly(ctx context.Context, user models.User, chatID, messageID, videoIDText, ownerIDText string) error {
	videoID, ownerID := parseID(videoIDText), parseID(ownerIDText)
	if videoID == 0 || ownerID == 0 {
		return nil
	}
	_ = s.max.DeleteMessage(ctx, chatID, messageID)
	if err := s.repo.CreateView(ctx, user.ID, videoID, ownerID, models.ActionLike); err != nil {
		return err
	}
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
		owner, err := s.repo.GetUserByID(ctx, ownerID)
		if err != nil {
			return err
		}
		if err := s.repo.CreateMatch(ctx, user.ID, ownerID); err != nil {
			return err
		}
		if err := s.sendContactAccess(ctx, owner.PlatformChatID, "❤️ У вас новый взаимный лайк!", user, true); err != nil {
			return err
		}
		return s.sendContactAccess(ctx, chatID, "❤️ У вас новый взаимный лайк!", *owner, true)
	}
	return s.SendNextCandidate(ctx, user)
}

func (s *DatingService) sendContactAccess(ctx context.Context, recipientID, title string, target models.User, includeMatches bool) error {
	if err := s.max.SendText(ctx, recipientID, title+"\n\n"+contactLineWithPhone(target), contactButtons(target, includeMatches)); err != nil {
		return err
	}
	if profileURL(target) == "" && strings.TrimSpace(target.ContactPhone) != "" {
		return s.max.SendContactCard(ctx, recipientID, displayName(target), target.ContactPhone)
	}
	return nil
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
	lines := []string{"📬 Взаимные лайки:", ""}
	buttons := [][]maxapi.Button{}
	for _, u := range users {
		lines = append(lines, displayName(u)+" | 🎥 Посмотреть кружок | 💬 Написать | ❌ Удалить")
		buttons = append(buttons, []maxapi.Button{{Text: "⚙️ " + shortName(u.Name), Payload: fmt.Sprintf("match_actions:%d", u.ID)}})
	}
	buttons = append(buttons,
		[]maxapi.Button{{Text: "▶️ Продолжить просмотр", Payload: "browse"}},
		[]maxapi.Button{{Text: "☰ Главное меню", Payload: "main_menu"}},
	)
	return s.max.SendText(ctx, user.PlatformChatID, strings.Join(lines, "\n"), buttons)
}

func (s *DatingService) matchVideoURL(ctx context.Context, userID int64) string {
	video, err := s.repo.GetActiveVideoByUser(ctx, userID)
	if err != nil {
		return ""
	}
	return normalizePublicURL(s.publicBaseURL, video.StorageURL)
}

func (s *DatingService) hideMatchURL(user models.User, otherUserID int64) string {
	if s.publicBaseURL == "" {
		return ""
	}
	query := url.Values{}
	query.Set("u", user.PlatformUserID)
	query.Set("m", fmt.Sprint(otherUserID))
	return s.publicBaseURL + "/matches/hide?" + query.Encode()
}

func (s *DatingService) SendMatchActions(ctx context.Context, user models.User, otherUserID int64) error {
	if otherUserID == 0 {
		return nil
	}
	other, err := s.repo.GetUserByID(ctx, otherUserID)
	if err != nil {
		return err
	}
	if _, err := s.repo.FindVisibleMatch(ctx, user.ID, otherUserID); err != nil {
		return s.max.SendText(ctx, user.PlatformChatID, "Этот контакт недоступен.", mainMenuButtons())
	}
	buttons := [][]maxapi.Button{}
	if link := s.matchVideoURL(ctx, otherUserID); link != "" {
		buttons = append(buttons, []maxapi.Button{{Text: "🎥 Посмотреть кружок", URL: link}})
	} else {
		buttons = append(buttons, []maxapi.Button{{Text: "🎥 Посмотреть кружок", Payload: fmt.Sprintf("match_video:%d", otherUserID)}})
	}
	if link := profileURL(*other); link != "" {
		buttons = append(buttons, []maxapi.Button{{Text: "💬 Написать", URL: link}})
	} else {
		buttons = append(buttons, []maxapi.Button{{Text: "💬 Ссылка недоступна", Payload: "missing_profile_link"}})
	}
	if link := s.hideMatchURL(user, otherUserID); link != "" {
		buttons = append(buttons, []maxapi.Button{{Text: "❌ Удалить", URL: link}})
	} else {
		buttons = append(buttons, []maxapi.Button{{Text: "❌ Удалить", Payload: fmt.Sprintf("hide_match:%d", otherUserID)}})
	}
	buttons = append(buttons,
		[]maxapi.Button{{Text: "📬 Взаимные лайки", Payload: "matches"}},
		[]maxapi.Button{{Text: "☰ Главное меню", Payload: "main_menu"}},
	)
	return s.max.SendText(ctx, user.PlatformChatID, "Действия для контакта: "+displayName(*other), buttons)
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

func (s *DatingService) SendMatchContactCard(ctx context.Context, user models.User, otherUserID int64) error {
	if otherUserID == 0 {
		return nil
	}
	if _, err := s.repo.FindVisibleMatch(ctx, user.ID, otherUserID); err != nil {
		return s.max.SendText(ctx, user.PlatformChatID, "Этот контакт недоступен.", mainMenuButtons())
	}
	other, err := s.repo.GetUserByID(ctx, otherUserID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(other.ContactPhone) == "" {
		return s.max.SendText(ctx, user.PlatformChatID, "У этого контакта нет сохраненного телефона.", mainMenuButtons())
	}
	return s.max.SendContactCard(ctx, user.PlatformChatID, displayName(*other), other.ContactPhone)
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
		"/admin_deeplink platform_user_id - тест deep link MAX",
		"/admin_deeplink_text platform_user_id - тест ссылок текстом",
		"/admin_phone_link_text phone - тест ссылок по телефону",
		"/admin_send_contact phone name - тест contact card",
		"/admin_send_forward user_id - тест пересланного сообщения",
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
	}, {
		{Text: "🔗 Тест deep link", Payload: fmt.Sprintf("admin:deeplink:%s", target.PlatformUserID)},
	}})
}

func (s *DatingService) SendAdminDeepLinkTest(ctx context.Context, admin models.User, platformUserID string) error {
	platformUserID = strings.TrimSpace(platformUserID)
	if platformUserID == "" {
		return s.max.SendText(ctx, admin.PlatformChatID, "Укажите platform_user_id:\n/admin_deeplink 5156654", nil)
	}
	shareText := url.QueryEscape("Привет! Это из бота «Знакомства кружки». У нас взаимный лайк 🙂")
	text := "Тест deep link для MAX user_id: " + platformUserID + "\n\n" +
		"Нажмите кнопки на телефоне и проверьте, какая откроет профиль или личный чат."
	return s.max.SendText(ctx, admin.PlatformChatID, text, [][]maxapi.Button{
		{{Text: "https user?id", URL: "https://max.ru/user?id=" + url.QueryEscape(platformUserID)}},
		{{Text: "https chat?user_id", URL: "https://max.ru/chat?user_id=" + url.QueryEscape(platformUserID)}},
		{{Text: "https /u/user_id", URL: "https://max.ru/u/" + url.PathEscape(platformUserID)}},
		{{Text: "https /id/user_id", URL: "https://max.ru/id" + url.PathEscape(platformUserID)}},
		{{Text: "share fallback", URL: "https://max.ru/:share?text=" + shareText}},
	})
}

func (s *DatingService) SendAdminDeepLinkTextTest(ctx context.Context, admin models.User, platformUserID string) error {
	platformUserID = strings.TrimSpace(platformUserID)
	if platformUserID == "" {
		return s.max.SendText(ctx, admin.PlatformChatID, "Укажите platform_user_id:\n/admin_deeplink_text 5156654", nil)
	}
	escaped := url.QueryEscape(platformUserID)
	text := strings.Join([]string{
		"Тест ссылок MAX текстом для user_id: " + platformUserID,
		"",
		"Проверьте ссылки прямо здесь, потом перешлите это сообщение в «Избранное» и проверьте еще раз.",
		"",
		"max://user?id=" + escaped,
		"max://chat?user_id=" + escaped,
		"https://max.ru/user?id=" + escaped,
		"https://max.ru/chat?user_id=" + escaped,
		"https://max.ru/u/" + url.PathEscape(platformUserID),
		"https://max.ru/id" + url.PathEscape(platformUserID),
		"",
		"Если один из вариантов откроет профиль или чат, напишите какой именно.",
	}, "\n")
	return s.max.SendText(ctx, admin.PlatformChatID, text, nil)
}

func (s *DatingService) SendAdminPhoneLinkTextTest(ctx context.Context, admin models.User, phone string) error {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return s.max.SendText(ctx, admin.PlatformChatID, "Укажите телефон:\n/admin_phone_link_text 79994589830", nil)
	}
	cleanPhone := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(phone)
	phoneNoPlus := strings.TrimPrefix(cleanPhone, "+")
	escaped := url.QueryEscape(cleanPhone)
	escapedNoPlus := url.QueryEscape(phoneNoPlus)
	text := strings.Join([]string{
		"Тест ссылок MAX текстом для телефона: " + cleanPhone,
		"",
		"Проверьте ссылки прямо здесь, потом перешлите это сообщение в «Избранное» и проверьте еще раз.",
		"",
		"tel:" + cleanPhone,
		"tel:+" + phoneNoPlus,
		"https://max.ru/phone/" + url.PathEscape(phoneNoPlus),
		"https://max.ru/phone/" + url.PathEscape(cleanPhone),
		"https://max.ru/contact?phone=" + escaped,
		"https://max.ru/contact?phone=" + escapedNoPlus,
		"https://max.ru/chat?phone=" + escaped,
		"https://max.ru/chat?phone=" + escapedNoPlus,
		"https://max.ru/user?phone=" + escaped,
		"https://max.ru/user?phone=" + escapedNoPlus,
		"",
		"Если один из вариантов откроет профиль или чат в MAX, напишите какой именно.",
	}, "\n")
	return s.max.SendText(ctx, admin.PlatformChatID, text, nil)
}

func (s *DatingService) SendAdminContactCardTest(ctx context.Context, admin models.User, input string) error {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 {
		return s.max.SendText(ctx, admin.PlatformChatID, "Укажите телефон и имя:\n/admin_send_contact 79994589830 Artem", nil)
	}
	phone := fields[0]
	name := "Test Contact"
	if len(fields) > 1 {
		name = strings.Join(fields[1:], " ")
	}
	results := s.max.SendContactCardTests(ctx, admin.PlatformChatID, name, phone)
	return s.max.SendText(ctx, admin.PlatformChatID, "Результат теста contact card:\n"+strings.Join(results, "\n"), nil)
}

func (s *DatingService) SendAdminForwardTest(ctx context.Context, admin models.User, platformUserID string) error {
	platformUserID = strings.TrimSpace(platformUserID)
	if platformUserID == "" {
		return s.max.SendText(ctx, admin.PlatformChatID, "Укажите user_id пользователя, который переслал сообщение:\n/admin_send_forward 4533898", nil)
	}
	forward, ok := s.getForward(platformUserID)
	if !ok {
		return s.max.SendText(ctx, admin.PlatformChatID, "Для этого user_id нет сохраненного пересланного сообщения. Сначала попросите пользователя переслать любое свое сообщение боту.", nil)
	}
	results := s.max.SendForwardTests(ctx, admin.PlatformChatID, forward)
	return s.max.SendText(ctx, admin.PlatformChatID, "Результат теста forward:\n"+strings.Join(results, "\n"), nil)
}

func (s *DatingService) saveForward(platformUserID string, forward maxapi.ForwardInfo) {
	if platformUserID == "" {
		return
	}
	s.forwardsMu.Lock()
	defer s.forwardsMu.Unlock()
	s.forwards[platformUserID] = forward
}

func (s *DatingService) getForward(platformUserID string) (maxapi.ForwardInfo, bool) {
	s.forwardsMu.RLock()
	defer s.forwardsMu.RUnlock()
	forward, ok := s.forwards[platformUserID]
	return forward, ok
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
	case "deeplink":
		if len(parts) == 3 {
			return s.SendAdminDeepLinkTest(ctx, user, parts[2])
		}
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

func contactComplete(user models.User) bool {
	return strings.TrimSpace(user.ProfileLink) != ""
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

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func contactLine(user models.User) string {
	return "Контакт: " + displayName(user)
}

func contactLineWithPhone(user models.User) string {
	line := "Контакт: " + displayName(user)
	if strings.TrimSpace(user.ContactPhone) != "" {
		line += "\nMAX телефон: " + strings.TrimSpace(user.ContactPhone)
	}
	return line
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

func normalizePublicURL(baseURL, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	if strings.HasPrefix(value, "/") && strings.TrimSpace(baseURL) != "" {
		return strings.TrimRight(baseURL, "/") + value
	}
	return value
}

func extractMaxProfileURL(value string) string {
	for _, part := range strings.Fields(value) {
		part = strings.Trim(part, " \t\r\n.,;:!?()[]{}<>\"'")
		if strings.HasPrefix(part, "https://max.ru/u/") || strings.HasPrefix(part, "http://max.ru/u/") {
			return part
		}
	}
	return value
}

func decodeQRFromImageURL(ctx context.Context, imageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 12 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("download qr image failed: %s", res.Status)
	}
	img, _, err := image.Decode(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return "", err
	}
	bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", err
	}
	result, err := qrcode.NewQRCodeReader().Decode(bitmap, nil)
	if err != nil {
		return "", err
	}
	return result.GetText(), nil
}

func validProfileLink(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "https://max.ru/u/") || strings.HasPrefix(value, "http://max.ru/u/")
}

func contactButtons(user models.User, includeMatches bool) [][]maxapi.Button {
	buttons := [][]maxapi.Button{}
	if url := profileURL(user); url != "" {
		buttons = append(buttons, []maxapi.Button{{Text: "💬 Написать " + shortName(user.Name), URL: url}})
	} else if strings.TrimSpace(user.ContactPhone) == "" {
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
		{
			{Text: "❤️ Лайк", Payload: fmt.Sprintf("like_only:%d:%d", videoID, ownerID)},
			{Text: "⏭ Следующий", Payload: fmt.Sprintf("next:%d:%d", videoID, ownerID)},
		},
		{{Text: "💬 Написать", Payload: fmt.Sprintf("like:%d:%d", videoID, ownerID)}},
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
		{{Text: "📤 Поделиться профилем MAX", Payload: "edit_profile_link"}},
		{{Text: "☰ Главное меню", Payload: "main_menu"}},
	}
}

func contactShareButtons() [][]maxapi.Button {
	return [][]maxapi.Button{
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

func (s *DatingService) premiumPriceText() string {
	price := strings.TrimSpace(s.premiumPrice)
	if price == "" {
		return "199 ₽"
	}
	price = strings.TrimSuffix(price, ".00")
	if strings.Contains(price, "₽") || strings.Contains(strings.ToLower(price), "руб") {
		return price
	}
	return price + " ₽"
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

func subscriptionStatusButtons() [][]maxapi.Button {
	return [][]maxapi.Button{
		{{Text: "🚫 Отписаться", Payload: "unsubscribe"}},
		{{Text: "☰ Главное меню", Payload: "main_menu"}},
	}
}
