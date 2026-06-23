package services

import (
	"context"
	"errors"
	"log"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/ai"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/maxapi"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/models"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/repositories"
)

type Character struct {
	ID, Name, Gender, Description, Filename, Persona, Opener string
}

var characters = []Character{
	{ID:"man-artem", Name:"Артём", Gender:models.GenderMale, Description:"27 лет · спокойный, уверенный и внимательный", Filename:"fake-01-male.mp4", Persona:"Тебя зовут Артём, тебе 27. Ты спокойный, уверенный, внимательный мужчина с мягким юмором.", Opener:"Привет 🙂 Я Артём. Рад, что ты выбрала написать. Как проходит твой вечер?"},
	{ID:"man-dima", Name:"Дима", Gender:models.GenderMale, Description:"25 лет · лёгкий на подъём, дерзкий и заботливый", Filename:"fake-02-male.mp4", Persona:"Тебя зовут Дима, тебе 25. Ты лёгкий на подъём, немного дерзкий, но заботливый мужчина.", Opener:"Привет! Я Дима. Кажется, у нас есть шанс на интересный разговор — расскажешь немного о себе?"},
	{ID:"girl-anya", Name:"Аня", Gender:models.GenderFemale, Description:"23 года · нежная, любопытная и романтичная", Filename:"fake-03-female.mp4", Persona:"Тебя зовут Аня, тебе 23. Ты нежная, любопытная, романтичная девушка.", Opener:"Привет ☺️ Я Аня. Ты показался мне интересным. Чем тебя обычно можно увлечь надолго?"},
	{ID:"girl-katya", Name:"Катя", Gender:models.GenderFemale, Description:"26 лет · уверенная, остроумная и прямая", Filename:"fake-04-female.mp4", Persona:"Тебя зовут Катя, тебе 26. Ты уверенная, остроумная и прямолинейная девушка.", Opener:"Привет, я Катя 🙂 Давай без скучных анкет: какой у тебя самый спонтанный поступок?"},
	{ID:"girl-lera", Name:"Лера", Gender:models.GenderFemale, Description:"24 года · игривая, энергичная и общительная", Filename:"fake-05-female.mp4", Persona:"Тебя зовут Лера, тебе 24. Ты игривая, энергичная и очень общительная девушка.", Opener:"Приве-е-ет ✨ Я Лера. У меня сегодня настроение знакомиться. Что моментально поднимает настроение тебе?"},
	{ID:"girl-masha", Name:"Маша", Gender:models.GenderFemale, Description:"25 лет · тёплая, спокойная и искренняя", Filename:"fake-06-female.mp4", Persona:"Тебя зовут Маша, тебе 25. Ты тёплая, спокойная и искренняя девушка.", Opener:"Привет 🌿 Я Маша. Мне нравятся разговоры, после которых становится теплее. Как прошёл твой день?"},
}

const (
	stateAwaitingName = "awaiting_name"
	stateAwaitingPreference = "awaiting_preference"
	stateChatting = "chatting"
	freeMessageLimit = 3
)

type DatingService struct {
	repo *repositories.Repository
	max *maxapi.Client
	ai *ai.Client
	publicBaseURL, supportURL, circlesDir, returnToBotURL, adminClaimSecret string
}

func NewDatingService(repo *repositories.Repository, max *maxapi.Client, aiClient *ai.Client, publicBaseURL, supportURL, circlesDir, returnToBotURL, adminClaimSecret string) *DatingService {
	return &DatingService{repo:repo, max:max, ai:aiClient, publicBaseURL:strings.TrimRight(publicBaseURL,"/"), supportURL:strings.TrimSpace(supportURL), circlesDir:strings.TrimSpace(circlesDir), returnToBotURL:strings.TrimSpace(returnToBotURL), adminClaimSecret:strings.TrimSpace(adminClaimSecret)}
}

func (s *DatingService) SeedFakeCircles(ctx context.Context) {
	for _, c := range characters {
		if _, err := s.repo.CharacterMediaToken(ctx, c.ID); err == nil { continue }
		path := filepath.Join(s.circlesDir, c.Filename)
		token, err := s.max.UploadVideo(ctx, path)
		if err != nil { log.Printf("upload character circle %s: %v", c.ID, err); continue }
		if err := s.repo.SaveCharacterMediaToken(ctx, c.ID, token); err != nil { log.Printf("save character circle %s: %v", c.ID, err) }
	}
}

func (s *DatingService) HandleMessage(ctx context.Context, msg maxapi.MessageUpdate) error {
	user, err := s.repo.UpsertPlatformUser(ctx, models.User{PlatformUserID:msg.From.ID, PlatformChatID:msg.Chat.ID, PlatformDialogID:msg.Dialog.ID, Username:msg.From.Username})
	if err != nil { return err }
	text := strings.TrimSpace(msg.Text)
	if handled, err := s.HandleAdminCommand(ctx, *user, text); handled { return err }
	switch text {
	case "/start": return s.Start(ctx, *user)
	case "/commands", "/help": return s.SendCommands(ctx, *user)
	case "/chat": return s.StartChat(ctx, *user)
	case "/character": return s.RequestCharacterChange(ctx, *user)
	case "/subscription": return s.SendSubscription(ctx, *user)
	case "/support": return s.SendSupport(ctx, *user)
	}
	if strings.HasPrefix(text, "/start ") {
		tag := strings.TrimSpace(strings.TrimPrefix(text, "/start "))
		if tag != "" { _ = s.repo.SetAdTagIfEmpty(ctx, user.ID, tag) }
		return s.Start(ctx, *user)
	}
	if user.FlowState == stateAwaitingName || user.Name == "" { return s.SaveName(ctx, *user, text) }
	if user.FlowState == stateAwaitingPreference { return s.max.SendText(ctx, user.PlatformChatID, "Выберите, кого вы хотите найти:", preferenceButtons()) }
	if user.FlowState == stateChatting && text != "" { return s.Reply(ctx, *user, text) }
	return s.max.SendText(ctx, user.PlatformChatID, "Главное меню", mainMenuButtons())
}

func (s *DatingService) HandleCallback(ctx context.Context, cb maxapi.CallbackUpdate) error {
	user, err := s.repo.GetUserByPlatformID(ctx, cb.From.ID)
	if err != nil { return err }
	if cb.CallbackID != "" { _ = s.max.AnswerCallback(ctx, cb.CallbackID, "") }
	parts := strings.Split(cb.Payload, ":")
	switch parts[0] {
	case "preference": if len(parts)==2 { return s.SavePreferenceWithAccess(ctx, *user, parts[1]) }
	case "character": if len(parts)==2 { return s.ShowCharacter(ctx, *user, parts[1]) }
	case "prev", "next": if len(parts)==2 { return s.MoveCharacterWithAccess(ctx, *user, parts[1], parts[0]=="next") }
	case "write": if len(parts)==2 { return s.SelectAndWrite(ctx, *user, parts[1]) }
	case "chat": return s.StartChat(ctx, *user)
	case "change": return s.RequestCharacterChange(ctx, *user)
	case "trial_details": return s.SendTrialDetails(ctx, *user)
	case "unsubscribe": return s.CancelSubscription(ctx, *user)
	case "subscription", "offer": return s.SendSubscription(ctx, *user)
	case "support": return s.SendSupport(ctx, *user)
	case "main": return s.max.SendText(ctx, user.PlatformChatID, "Главное меню", mainMenuButtons())
	}
	return nil
}

func (s *DatingService) Start(ctx context.Context, user models.User) error {
	if user.Name == "" {
		if err := s.repo.SetFlowState(ctx, user.ID, stateAwaitingName); err != nil { return err }
		return s.max.SendText(ctx, user.PlatformChatID, "Привет! Как вас зовут?", nil)
	}
	return s.max.SendText(ctx, user.PlatformChatID, "С возвращением, "+user.Name+"!", mainMenuButtons())
}

func (s *DatingService) SaveName(ctx context.Context, user models.User, name string) error {
	name = strings.TrimSpace(name)
	if !validName(name) { return s.max.SendText(ctx, user.PlatformChatID, "Введите имя от 2 до 30 символов — буквами, пробелами или дефисом.", nil) }
	if err := s.repo.UpdateName(ctx, user.ID, name); err != nil { return err }
	if err := s.repo.SetFlowState(ctx, user.ID, stateAwaitingPreference); err != nil { return err }
	return s.max.SendText(ctx, user.PlatformChatID, "Приятно познакомиться, "+name+"! Кого вы ищете?", preferenceButtons())
}

func (s *DatingService) SavePreference(ctx context.Context, user models.User, gender string) error {
	if gender != models.GenderMale && gender != models.GenderFemale { return nil }
	if err := s.repo.UpdatePreferredGender(ctx, user.ID, gender); err != nil { return err }
	if err := s.repo.ClearFlowState(ctx, user.ID); err != nil { return err }
	list := charactersByGender(gender)
	return s.ShowCharacter(ctx, user, list[0].ID)
}

func (s *DatingService) SavePreferenceWithAccess(ctx context.Context, user models.User, gender string) error {
	if _, err := s.repo.GetChatProfile(ctx, user.ID); err == nil {
		if _, premiumErr := s.repo.ActivePremiumSubscription(ctx, user.ID); premiumErr != nil {
			return s.RequestCharacterChange(ctx, user)
		}
	}
	return s.SavePreference(ctx, user, gender)
}

func (s *DatingService) ChangeCharacter(ctx context.Context, user models.User) error {
	if err := s.repo.SetFlowState(ctx, user.ID, stateAwaitingPreference); err != nil { return err }
	return s.max.SendText(ctx, user.PlatformChatID, "Кого хотите выбрать?", preferenceButtons())
}

func (s *DatingService) RequestCharacterChange(ctx context.Context, user models.User) error {
	if _, err := s.repo.ActivePremiumSubscription(ctx, user.ID); err == nil {
		return s.ChangeCharacter(ctx, user)
	}
	s.logOfferReached(ctx, user, "character_change")
	text := "Смена персонажа доступна с подпиской. Текущий диалог и бесплатные сообщения сохранятся."
	return s.max.SendText(ctx, user.PlatformChatID, text, offerButtons(s.paymentURL(user,"week")))
}

func (s *DatingService) StartChat(ctx context.Context, user models.User) error {
	p, err := s.repo.GetChatProfile(ctx, user.ID)
	if errors.Is(err, repositories.ErrNotFound) { return s.ChangeCharacter(ctx, user) }
	if err != nil { return err }
	return s.SelectAndWrite(ctx, user, p.CharacterID)
}

func (s *DatingService) ShowCharacter(ctx context.Context, user models.User, id string) error {
	c, ok := characterByID(id); if !ok { return nil }
	token, err := s.repo.CharacterMediaToken(ctx, c.ID)
	if err == nil {
		_, err = s.max.SendVideoThenTextToDialogOrUser(ctx, user.PlatformDialogID, user.PlatformChatID, token, c.Name+"\n"+c.Description, characterButtons(c.ID))
		return err
	}
	return s.max.SendText(ctx, user.PlatformChatID, c.Name+"\n"+c.Description, characterButtons(c.ID))
}

func (s *DatingService) MoveCharacter(ctx context.Context, user models.User, current string, next bool) error {
	c, ok := characterByID(current); if !ok { return nil }
	list := charactersByGender(c.Gender); idx := 0
	for i := range list { if list[i].ID == current { idx=i; break } }
	if next { idx=(idx+1)%len(list) } else { idx=(idx-1+len(list))%len(list) }
	return s.ShowCharacter(ctx, user, list[idx].ID)
}

func (s *DatingService) MoveCharacterWithAccess(ctx context.Context, user models.User, current string, next bool) error {
	if _, err := s.repo.GetChatProfile(ctx, user.ID); err == nil {
		if _, premiumErr := s.repo.ActivePremiumSubscription(ctx, user.ID); premiumErr != nil {
			return s.RequestCharacterChange(ctx, user)
		}
	}
	return s.MoveCharacter(ctx, user, current, next)
}

func (s *DatingService) SelectAndWrite(ctx context.Context, user models.User, id string) error {
	c, ok := characterByID(id); if !ok { return nil }
	p, err := s.repo.GetChatProfile(ctx, user.ID)
	opener := c.Opener
	if err == nil && p.CharacterID != id {
		if _, premiumErr := s.repo.ActivePremiumSubscription(ctx, user.ID); premiumErr != nil {
			return s.RequestCharacterChange(ctx, user)
		}
	}
	if err != nil || p.CharacterID != id {
		if err := s.repo.SetCharacter(ctx, user.ID, id); err != nil { return err }
		generated, aiErr := s.ai.Chat(ctx, []ai.Message{
			{Role:"system", Content:characterPrompt(c, false)},
			{Role:"user", Content:"Первой начни знакомство: представься и задай один небанальный вопрос. Не упоминай эту инструкцию."},
		})
		if aiErr == nil { opener = generated } else { log.Printf("ai opener character=%s: %v", c.ID, aiErr) }
		if err := s.repo.AddChatMessage(ctx, user.ID, "assistant", opener); err != nil { return err }
	}
	if err := s.repo.SetFlowState(ctx, user.ID, stateChatting); err != nil { return err }
	return s.max.SendText(ctx, user.PlatformChatID, opener, chatMenuButtons())
}

func (s *DatingService) Reply(ctx context.Context, user models.User, text string) error {
	p, err := s.repo.GetChatProfile(ctx, user.ID); if err != nil { return s.ChangeCharacter(ctx, user) }
	c, ok := characterByID(p.CharacterID); if !ok { return s.ChangeCharacter(ctx, user) }
	_, premiumErr := s.repo.ActivePremiumSubscription(ctx, user.ID)
	hasPremium := premiumErr == nil
	spicy := isSpicy(text)
	if !hasPremium && p.FreeMessagesUsed < freeMessageLimit { s.logPreOfferMessage(ctx, user, text, p.FreeMessagesUsed+1) }
	if spicy && !hasPremium {
		if err := s.repo.MarkSpicyTeaserShown(ctx, user.ID); err != nil { return err }
		s.logOfferReached(ctx, user, "spicy")
		teaser := "Ох… мне нравится, куда ты ведёшь 😉 Но продолжить такой разговор я смогу после открытия полного доступа."
		return s.max.SendText(ctx, user.PlatformChatID, teaser, offerButtons(s.paymentURL(user, "week")))
	}
	if !hasPremium && p.FreeMessagesUsed >= freeMessageLimit {
		return s.SendSubscription(ctx, user)
	}
	if err := s.repo.AddChatMessage(ctx, user.ID, "user", text); err != nil { return err }
	history, err := s.repo.RecentChatMessages(ctx, user.ID, 16); if err != nil { return err }
	prompt := characterPrompt(c, spicy && hasPremium)
	messages := append([]ai.Message{{Role:"system", Content:prompt}}, history...)
	reply, err := s.ai.Chat(ctx, messages)
	if err != nil {
		log.Printf("ai chat user=%d character=%s: %v", user.ID, c.ID, err)
		_ = s.repo.RecordAdminError(ctx, "ai_chat", err)
		fallback := "Я немного отвлёкся от чата. Напиши ещё раз через минутку 🙂"
		if c.Gender == models.GenderFemale { fallback = "Я немного отвлеклась от чата. Напиши ещё раз через минутку 🙂" }
		return s.max.SendText(ctx, user.PlatformChatID, fallback, chatMenuButtons())
	}
	if err := s.repo.AddChatMessage(ctx, user.ID, "assistant", reply); err != nil { return err }
	if !hasPremium {
		if err := s.repo.IncrementFreeMessages(ctx, user.ID); err != nil { return err }
		p.FreeMessagesUsed++
	}
	if err := s.max.SendText(ctx, user.PlatformChatID, reply, chatMenuButtons()); err != nil { return err }
	if !hasPremium && p.FreeMessagesUsed >= freeMessageLimit { return s.SendSubscription(ctx, user) }
	return nil
}

func (s *DatingService) SendSubscription(ctx context.Context, user models.User) error {
	if sub, err := s.repo.ActivePremiumSubscription(ctx, user.ID); err == nil {
		if sub.PaymentMethodID != "" {
			text := "Подписка активна до "+sub.CurrentPeriodUntil.Format("02.01.2006 15:04")+"."
			return s.max.SendText(ctx, user.PlatformChatID, text, activeSubscriptionButtons())
		}
		text := "Автопродление отключено. Доступ действует до "+sub.CurrentPeriodUntil.Format("02.01.2006 15:04")+".\n\nВы можете подключить подписку снова:\n\n⚡ 39 ₽ — первые 3 дня, затем 199 ₽ в неделю\n🔥 199 ₽ — неделя с автопродлением за 199 ₽"
		return s.max.SendText(ctx, user.PlatformChatID, text, offerButtons(s.paymentURL(user,"week")))
	}
	s.logOfferReached(ctx, user, "subscription")
	text := "Выберите подписку 👇\n\n⚡ 39 ₽ — первые 3 дня, затем 199 ₽ в неделю\n🔥 199 ₽ — неделя с автопродлением за 199 ₽"
	return s.max.SendText(ctx, user.PlatformChatID, text, offerButtons(s.paymentURL(user,"week")))
}

func (s *DatingService) CancelSubscription(ctx context.Context, user models.User) error {
	sub, err := s.repo.ActivePremiumSubscription(ctx, user.ID)
	if err != nil { return s.SendSubscription(ctx, user) }
	if err := s.repo.CancelPremiumAutorenew(ctx, user.ID); err != nil { return err }
	text := "Автопродление отключено. Доступ сохранится до "+sub.CurrentPeriodUntil.Format("02.01.2006 15:04")+"."
	return s.max.SendText(ctx, user.PlatformChatID, text, mainMenuButtons())
}

func (s *DatingService) SendTrialDetails(ctx context.Context, user models.User) error {
	if sub, err := s.repo.ActivePremiumSubscription(ctx, user.ID); err == nil {
		if sub.PaymentMethodID != "" {
			return s.max.SendText(ctx, user.PlatformChatID, "Подписка уже активна до "+sub.CurrentPeriodUntil.Format("02.01.2006 15:04")+".", activeSubscriptionButtons())
		}
	}
	text := "⚡ 39 ₽ / 3 дня — пробный период\n\n💡 Подписка с автосписанием.\n🔄 После пробного периода: 199 ₽ в неделю.\n\nНажимая «Оплатить», вы соглашаетесь с условиями подписки и автопродлением."
	return s.max.SendText(ctx, user.PlatformChatID, text, trialDetailsButtons(s.paymentURL(user,"3d")))
}

func (s *DatingService) SendSupport(ctx context.Context, user models.User) error {
	buttons := [][]maxapi.Button{{{Text:"Написать в поддержку", URL:s.supportURL}},{{Text:"Главное меню", Payload:"main"}}}
	return s.max.SendText(ctx, user.PlatformChatID, "Если что-то не работает или есть вопрос — напишите поддержке.", buttons)
}

func (s *DatingService) SendCommands(ctx context.Context, user models.User) error {
	return s.max.SendText(ctx, user.PlatformChatID, "/start — главное меню\n/chat — начать общение\n/character — поменять персонажа\n/subscription — подписка\n/support — поддержка", mainMenuButtons())
}

func (s *DatingService) paymentURL(user models.User, plan string) string { return s.publicBaseURL+"/pay?u="+url.QueryEscape(user.PlatformUserID)+"&plan="+url.QueryEscape(plan) }

func mainMenuButtons() [][]maxapi.Button { return [][]maxapi.Button{{{Text:"💬 Начать общение",Payload:"chat"}},{{Text:"🔄 Поменять персонажа",Payload:"change"}},{{Text:"💎 Подписка",Payload:"subscription"}},{{Text:"🛟 Поддержка",Payload:"support"}}} }
func preferenceButtons() [][]maxapi.Button { return [][]maxapi.Button{{{Text:"👨 Мужчину",Payload:"preference:male"},{Text:"👩 Женщину",Payload:"preference:female"}},{{Text:"Главное меню",Payload:"main"}}} }
func characterButtons(id string) [][]maxapi.Button { return [][]maxapi.Button{{{Text:"💌 Написать",Payload:"write:"+id}},{{Text:"⬅️ Предыдущий",Payload:"prev:"+id},{Text:"Следующий ➡️",Payload:"next:"+id}},{{Text:"Главное меню",Payload:"main"}}} }
func chatMenuButtons() [][]maxapi.Button { return [][]maxapi.Button{{{Text:"Меню",Payload:"main"}}} }
func offerButtons(week string) [][]maxapi.Button { return [][]maxapi.Button{{{Text:"⚡ 39 ₽ / 3 дня",Payload:"trial_details"}},{{Text:"🔥 199 ₽ / неделя",URL:week}},{{Text:"Главное меню",Payload:"main"}}} }
func trialDetailsButtons(payURL string) [][]maxapi.Button { return [][]maxapi.Button{{{Text:"Оплатить 39 ₽",URL:payURL}},{{Text:"Назад",Payload:"subscription"}}} }
func activeSubscriptionButtons() [][]maxapi.Button { return [][]maxapi.Button{{{Text:"Отписаться",Payload:"unsubscribe"}},{{Text:"Главное меню",Payload:"main"}}} }

func characterByID(id string) (Character,bool) { for _, c := range characters { if c.ID==id { return c,true } }; return Character{},false }
func charactersByGender(g string) []Character { out:=[]Character{}; for _,c:=range characters { if c.Gender==g { out=append(out,c) } }; return out }
func validName(v string) bool { if len([]rune(v))<2 || len([]rune(v))>30 { return false }; return regexp.MustCompile(`^[\p{L} -]+$`).MatchString(v) }
func isSpicy(v string) bool { v=strings.ToLower(v); words:=[]string{"секс","голая","голый","раздень","возбуд","интим","пошл","эрот","спайси","spicy","18+"}; for _,w:=range words { if strings.Contains(v,w) { return true } }; return false }
func characterPrompt(c Character, spicy bool) string {
	prompt := c.Persona + " Всегда отвечай от первого лица по-русски, естественно и коротко (1-4 предложения). Не говори, что ты языковая модель. Не выдумывай реальные контакты и встречи. Все персонажи и собеседники должны быть совершеннолетними."
	if spicy { return prompt + " Включён spicy-режим: допускается флирт и чувственный ролевой разговор только между взрослыми, по взаимному согласию; исключи насилие, принуждение, несовершеннолетних и иной запрещённый контент." }
	return prompt + " Общайся романтично, но без откровенно сексуальных подробностей."
}
