package services

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/models"
)

const adminMenu = `Админ-меню
/admin_claim секрет - добавить себя первым админом
/adstats метка - статистика по метке
/adstats_all - статистика по всем меткам
/botstats - общая статистика
/substats - статистика подписок
/choicestats - статистика выбора персонажей
/tester_spend_free - потратить свои бесплатные сообщения
/tester_reset_me - очистить свой профиль, сохранив админку
/adtag метка - создать ссылку с меткой
/push_leads [лимит] - пуш пользователям без активной подписки
/push_active текст - пуш активным пользователям и админам
/push_stats - диагностика базы и последнего пуша
/payments - последние оплаты
/errors - последние ошибки
/user id - карточка пользователя
/admin_add id - добавить админа
/admin_del id - удалить админа
/admin_list - список админов
/admin_reset_payments all|id - сбросить тестовые оплаты
/admin_reset_store confirm - полностью очистить базу бота`

func isAdminCommand(text string) bool {
	if !strings.HasPrefix(text,"/") { return false }
	command:=strings.Fields(text)[0]
	switch command {
	case "/admin","/menu","/help","/admin_claim","/adstats","/adstats_all","/botstats","/substats","/choicestats","/tester_spend_free","/tester_reset_me","/adtag","/push_leads","/push_active","/push_stats","/payments","/errors","/user","/admin_add","/admin_del","/admin_list","/admin_reset_payments","/admin_reset_store": return true
	}
	return false
}

func (s *DatingService) HandleAdminCommand(ctx context.Context,user models.User,text string)(bool,error){
	if !isAdminCommand(text){return false,nil}
	parts:=strings.Fields(text);command:=parts[0];args:=parts[1:]
	if command=="/admin_claim"{
		if s.adminClaimSecret=="" || len(args)!=1 || args[0]!=s.adminClaimSecret{return true,s.max.SendText(ctx,user.PlatformChatID,"Неверный секрет или команда отключена.",nil)}
		ok,err:=s.repo.AdminClaim(ctx,user.PlatformUserID);if err!=nil{return true,err};if !ok{return true,s.max.SendText(ctx,user.PlatformChatID,"Первый администратор уже назначен.",nil)}
		return true,s.max.SendText(ctx,user.PlatformChatID,"Администратор добавлен.\n\n"+adminMenu,nil)
	}
	isAdmin,err:=s.repo.IsAdmin(ctx,user.PlatformUserID);if err!=nil{return true,err}
	if !isAdmin{if command=="/help"{return false,nil};return true,s.max.SendText(ctx,user.PlatformChatID,"Команда доступна только администратору.",nil)}
	send:=func(text string)(bool,error){return true,s.max.SendText(ctx,user.PlatformChatID,text,nil)}
	switch command{
	case "/admin","/menu","/help": return send(adminMenu)
	case "/admin_list": ids,err:=s.repo.ListAdmins(ctx);if err!=nil{return true,err};return send("Администраторы:\n"+strings.Join(ids,"\n"))
	case "/admin_add": if len(args)!=1{return send("Использование: /admin_add id")};if err:=s.repo.AddAdmin(ctx,args[0]);err!=nil{return true,err};return send("Администратор добавлен: "+args[0])
	case "/admin_del": if len(args)!=1{return send("Использование: /admin_del id")};if args[0]==user.PlatformUserID{return send("Нельзя удалить самого себя.")};if err:=s.repo.DeleteAdmin(ctx,args[0]);err!=nil{return true,err};return send("Администратор удалён: "+args[0])
	case "/botstats": stats,err:=s.repo.AdminBotStats(ctx);if err!=nil{return true,err};revenue,err:=s.repo.AdminRevenue(ctx);if err!=nil{return true,err};return send(fmt.Sprintf("Общая статистика\nПользователи: %d\nПрофили: %d\nСообщения: %d\nАктивные подписки: %d\nПокупатели: %d\nУспешные оплаты: %d\nВыручка: %.2f ₽",stats["users"],stats["profiles"],stats["messages"],stats["active_subscriptions"],stats["buyers"],stats["payments"],revenue))
	case "/substats": stats,err:=s.repo.AdminSubscriptionStats(ctx);if err!=nil{return true,err};return send(fmt.Sprintf("Подписки\nАктивны с автопродлением: %d\nАктивны после отмены: %d\nИстекли: %d\nОплаты 39 ₽: %d\nОплаты 199 ₽: %d\nАвтопродления: %d",stats["active_autorenew"],stats["active_canceled"],stats["expired"],stats["trial_payments"],stats["week_payments"],stats["renewals"]))
	case "/choicestats": stats,err:=s.repo.AdminChoiceStats(ctx);if err!=nil{return true,err};lines:=[]string{"Выбор персонажей:"};for _,c:=range characters{lines=append(lines,fmt.Sprintf("%s: %d",c.Name,stats[c.ID]))};return send(strings.Join(lines,"\n"))
	case "/adstats","/adstats_all": tag:="";if command=="/adstats"{if len(args)!=1{return send("Использование: /adstats метка")};tag=args[0]};stats,err:=s.repo.AdStats(ctx,tag);if err!=nil{return true,err};if len(stats)==0{return send("По этой метке данных пока нет.")};lines:=[]string{"Статистика рекламы:"};for _,x:=range stats{lines=append(lines,fmt.Sprintf("%s — пользователи %d, оффер %d, покупатели %d, сумма %.2f ₽",x.Tag,x.Users,x.Offer,x.Buyers,x.Sum))};return send(strings.Join(lines,"\n"))
	case "/adtag": if len(args)!=1{return send("Использование: /adtag метка")};base:=s.returnToBotURL;sep:="?";if strings.Contains(base,"?"){sep="&"};return send("Метка: "+args[0]+"\nСсылка: "+base+sep+"start="+url.QueryEscape(args[0]))
	case "/tester_spend_free": if err:=s.repo.SpendFreeMessages(ctx,user.ID,freeMessageLimit);err!=nil{return true,err};return send("Бесплатные сообщения потрачены. Можно проверять оффер.")
	case "/tester_reset_me": if err:=s.repo.ResetAIUser(ctx,user.ID);err!=nil{return true,err};return send("Профиль очищен. Права администратора сохранены. Нажмите /start.")
	case "/payments": items,err:=s.repo.RecentAdminPayments(ctx,10);if err!=nil{return true,err};if len(items)==0{return send("Оплат пока нет.")};lines:=[]string{"Последние оплаты:"};for _,p:=range items{lines=append(lines,fmt.Sprintf("%s · %s ₽ · %s · %s · %s",p.PlatformUserID,p.Amount,p.Status,p.Plan,p.CreatedAt.Format("02.01 15:04")))};return send(strings.Join(lines,"\n"))
	case "/errors": items,err:=s.repo.RecentAdminErrors(ctx,10);if err!=nil{return true,err};if len(items)==0{return send("Ошибок пока нет.")};lines:=[]string{"Последние ошибки:"};for _,e:=range items{lines=append(lines,fmt.Sprintf("%s · %s · %s",e.CreatedAt.Format("02.01 15:04"),e.Source,truncateAdmin(e.Message,180)))};return send(strings.Join(lines,"\n"))
	case "/user": if len(args)!=1{return send("Использование: /user id")};target,err:=s.repo.GetUserByPlatformID(ctx,args[0]);if err!=nil{return send("Пользователь не найден.")};subText:="нет";if sub,e:=s.repo.ActivePremiumSubscription(ctx,target.ID);e==nil{subText="до "+sub.CurrentPeriodUntil.Format("02.01.2006 15:04");if sub.PaymentMethodID==""{subText+=" (автопродление отключено)"}};return send(fmt.Sprintf("Пользователь %s\nID базы: %d\nИмя: %s\nПерсонаж/статус: %s\nПодписка: %s\nСоздан: %s",target.PlatformUserID,target.ID,target.Name,target.FlowState,subText,target.CreatedAt.Format("02.01.2006 15:04")))
	case "/admin_reset_payments": if len(args)!=1{return send("Использование: /admin_reset_payments all или id")};count,err:=s.repo.ResetPremiumPayments(ctx,args[0]);if err!=nil{return true,err};return send(fmt.Sprintf("Готово. Сброшено платежей: %d.",count))
	case "/admin_reset_store": if len(args)!=1||args[0]!="confirm"{return send("Использование: /admin_reset_store confirm")};if err:=s.repo.ClearAllData(ctx);err!=nil{return true,err};return send("База бота очищена. Список администраторов сохранён.")
	case "/push_stats": stats,err:=s.repo.AdminPushDiagnostics(ctx);if err!=nil{return true,err};last:="Запусков ещё не было.";if stats.CreatedAt!=nil{last=fmt.Sprintf("Последний пуш %s: %d/%d, ошибок %d, %s",stats.Kind,stats.Succeeded,stats.Total,stats.Failed,stats.CreatedAt.Format("02.01 15:04"))};return send(fmt.Sprintf("Диагностика пушей\nПользователи: %d\nБез подписки: %d\nАктивные подписки: %d\n%s",stats.Users,stats.Leads,stats.Active,last))
	case "/push_leads": limit:=0;if len(args)>0{limit,_=strconv.Atoi(args[0]);if limit<0{limit=0}};targets,err:=s.repo.AdminPushTargets(ctx,"leads",limit);if err!=nil{return true,err};ok,failed:=s.sendAdminPush(ctx,targets,"Вернись в чат — персонаж уже ждёт твоего сообщения 💌",true);_ = s.repo.SavePushRun(ctx,"leads",len(targets),ok,failed);return send(fmt.Sprintf("Пуш завершён: успешно %d/%d, ошибок %d.",ok,len(targets),failed))
	case "/push_active": message:=strings.TrimSpace(strings.TrimPrefix(text,command));if message==""{return send("Использование: /push_active текст")};targets,err:=s.repo.AdminPushTargets(ctx,"active",0);if err!=nil{return true,err};ok,failed:=s.sendAdminPush(ctx,targets,message,false);_ = s.repo.SavePushRun(ctx,"active",len(targets),ok,failed);return send(fmt.Sprintf("Пуш завершён: успешно %d/%d, ошибок %d.",ok,len(targets),failed))
	}
	return true,nil
}

func(s *DatingService)sendAdminPush(ctx context.Context,users []models.User,text string,offer bool)(int,int){ok,failed:=0,0;for _,u:=range users{var err error;if offer{err=s.max.SendText(ctx,u.PlatformChatID,text,offerButtons(s.paymentURL(u,"week")))}else{err=s.max.SendText(ctx,u.PlatformChatID,text,nil)};if err!=nil{failed++;_ = s.repo.RecordAdminError(ctx,"push",err)}else{ok++}};return ok,failed}
func truncateAdmin(v string,n int)string{r:=[]rune(v);if len(r)<=n{return v};return string(r[:n])+"…"}
func(s *DatingService)RecordError(ctx context.Context,source string,err error){_ = s.repo.RecordAdminError(ctx,source,err)}
