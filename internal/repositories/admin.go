package repositories

import (
	"context"
	"fmt"
	"time"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/models"
)

type AdminPayment struct {
	PlatformUserID, Amount, Status, Plan, Reason string
	CreatedAt time.Time
}

type AdminError struct { Source, Message string; CreatedAt time.Time }
type AdminPushStats struct { Users, Leads, Active, Total, Succeeded, Failed int64; Kind string; CreatedAt *time.Time }

func (r *Repository) SeedAdmins(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if id == "" { continue }
		if _, err := r.db.Exec(ctx, `insert into bot_admins(platform_user_id) values ($1) on conflict do nothing`, id); err != nil { return err }
	}
	return nil
}

func (r *Repository) IsAdmin(ctx context.Context, platformID string) (bool, error) {
	var ok bool
	err := r.db.QueryRow(ctx, `select exists(select 1 from bot_admins where platform_user_id=$1)`, platformID).Scan(&ok)
	return ok, err
}

func (r *Repository) AddAdmin(ctx context.Context, platformID string) error {
	_, err := r.db.Exec(ctx, `insert into bot_admins(platform_user_id) values ($1) on conflict do nothing`, platformID)
	return err
}

func (r *Repository) DeleteAdmin(ctx context.Context, platformID string) error {
	_, err := r.db.Exec(ctx, `delete from bot_admins where platform_user_id=$1`, platformID)
	return err
}

func (r *Repository) ListAdmins(ctx context.Context) ([]string, error) {
	rows, err := r.db.Query(ctx, `select platform_user_id from bot_admins order by created_at, platform_user_id`)
	if err != nil { return nil, err }
	defer rows.Close()
	out := []string{}
	for rows.Next() { var id string; if err := rows.Scan(&id); err != nil { return nil, err }; out=append(out,id) }
	return out, rows.Err()
}

func (r *Repository) AdminClaim(ctx context.Context, platformID string) (bool, error) {
	var count int
	if err := r.db.QueryRow(ctx, `select count(*) from bot_admins`).Scan(&count); err != nil { return false, err }
	if count > 0 { return false, nil }
	return true, r.AddAdmin(ctx, platformID)
}

func (r *Repository) AdminBotStats(ctx context.Context) (map[string]int64, error) {
	queries := map[string]string{
		"users": `select count(*) from users where platform_user_id not like 'fake_circle_%'`,
		"profiles": `select count(*) from chat_profiles`,
		"messages": `select count(*) from chat_messages`,
		"active_subscriptions": `select count(*) from premium_subscriptions where active=true and current_period_until>now()`,
		"buyers": `select count(distinct user_id) from premium_payments where status='succeeded'`,
		"payments": `select count(*) from premium_payments where status='succeeded'`,
	}
	out := map[string]int64{}
	for key,q := range queries { var value int64; if err:=r.db.QueryRow(ctx,q).Scan(&value); err!=nil{return nil,err}; out[key]=value }
	return out,nil
}

func (r *Repository) AdminRevenue(ctx context.Context) (float64,error) {
	var value float64
	err:=r.db.QueryRow(ctx,`select coalesce(sum(amount),0)::float8 from premium_payments where status='succeeded'`).Scan(&value)
	return value,err
}

func (r *Repository) AdminSubscriptionStats(ctx context.Context) (map[string]int64,error) {
	queries:=map[string]string{
		"active_autorenew":`select count(*) from premium_subscriptions where active=true and current_period_until>now() and payment_method_id is not null`,
		"active_canceled":`select count(*) from premium_subscriptions where active=true and current_period_until>now() and payment_method_id is null`,
		"expired":`select count(*) from premium_subscriptions where active=false or current_period_until<=now()`,
		"trial_payments":`select count(*) from premium_payments where status='succeeded' and plan='3d'`,
		"week_payments":`select count(*) from premium_payments where status='succeeded' and plan='week'`,
		"renewals":`select count(*) from premium_payments where status='succeeded' and reason='renewal'`,
	}
	out:=map[string]int64{}
	for key,q:=range queries{var v int64;if err:=r.db.QueryRow(ctx,q).Scan(&v);err!=nil{return nil,err};out[key]=v}
	return out,nil
}

func (r *Repository) AdminChoiceStats(ctx context.Context) (map[string]int64,error) {
	rows,err:=r.db.Query(ctx,`select character_id,count(*) from chat_profiles group by character_id order by count(*) desc`)
	if err!=nil{return nil,err};defer rows.Close();out:=map[string]int64{}
	for rows.Next(){var id string;var n int64;if err:=rows.Scan(&id,&n);err!=nil{return nil,err};out[id]=n}
	return out,rows.Err()
}

func (r *Repository) SpendFreeMessages(ctx context.Context,userID int64,limit int) error {
	_,err:=r.db.Exec(ctx,`insert into chat_profiles(user_id,character_id,free_messages_used) values($1,'girl-anya',$2) on conflict(user_id) do update set free_messages_used=$2,updated_at=now()`,userID,limit)
	return err
}

func (r *Repository) ResetAIUser(ctx context.Context,userID int64) error {
	tx,err:=r.db.Begin(ctx);if err!=nil{return err};defer tx.Rollback(ctx)
	for _,q:=range []string{`delete from chat_messages where user_id=$1`,`delete from chat_profiles where user_id=$1`,`delete from premium_payments where user_id=$1`,`delete from premium_subscriptions where user_id=$1`}{if _,err:=tx.Exec(ctx,q,userID);err!=nil{return err}}
	if _,err:=tx.Exec(ctx,`update users set name=null,preferred_gender=null,flow_state='',is_premium=false,updated_at=now() where id=$1`,userID);err!=nil{return err}
	return tx.Commit(ctx)
}

func (r *Repository) ResetPremiumPayments(ctx context.Context,platformID string) (int64,error) {
	tx,err:=r.db.Begin(ctx);if err!=nil{return 0,err};defer tx.Rollback(ctx)
	condition:="";args:=[]any{}
	if platformID!="all"{condition=" where platform_user_id=$1";args=append(args,platformID)}
	rows,err:=tx.Query(ctx,`select id from users`+condition,args...);if err!=nil{return 0,err};ids:=[]int64{}
	for rows.Next(){var id int64;if err:=rows.Scan(&id);err!=nil{rows.Close();return 0,err};ids=append(ids,id)};rows.Close()
	var total int64
	for _,id:=range ids{tag,err:=tx.Exec(ctx,`delete from premium_payments where user_id=$1`,id);if err!=nil{return 0,err};total+=tag.RowsAffected();if _,err:=tx.Exec(ctx,`delete from premium_subscriptions where user_id=$1`,id);err!=nil{return 0,err};if _,err:=tx.Exec(ctx,`update users set is_premium=false,updated_at=now() where id=$1`,id);err!=nil{return 0,err}}
	return total,tx.Commit(ctx)
}

func (r *Repository) RecentAdminPayments(ctx context.Context,limit int)([]AdminPayment,error){
	rows,err:=r.db.Query(ctx,`select u.platform_user_id,p.amount::text,p.status,coalesce(p.plan,''),p.reason,p.created_at from premium_payments p join users u on u.id=p.user_id order by p.created_at desc limit $1`,limit);if err!=nil{return nil,err};defer rows.Close();out:=[]AdminPayment{}
	for rows.Next(){var p AdminPayment;if err:=rows.Scan(&p.PlatformUserID,&p.Amount,&p.Status,&p.Plan,&p.Reason,&p.CreatedAt);err!=nil{return nil,err};out=append(out,p)};return out,rows.Err()
}

func (r *Repository) RecordAdminError(ctx context.Context,source string,err error) error { if err==nil{return nil};_,e:=r.db.Exec(ctx,`insert into admin_error_logs(source,message) values($1,$2)`,source,err.Error());return e }
func (r *Repository) RecentAdminErrors(ctx context.Context,limit int)([]AdminError,error){rows,err:=r.db.Query(ctx,`select source,message,created_at from admin_error_logs order by created_at desc limit $1`,limit);if err!=nil{return nil,err};defer rows.Close();out:=[]AdminError{};for rows.Next(){var e AdminError;if err:=rows.Scan(&e.Source,&e.Message,&e.CreatedAt);err!=nil{return nil,err};out=append(out,e)};return out,rows.Err()}

func (r *Repository) AdminPushTargets(ctx context.Context,kind string,limit int)([]models.User,error){
	where:=`not exists(select 1 from premium_subscriptions ps where ps.user_id=u.id and ps.active=true and ps.current_period_until>now())`
	if kind=="active"{where=`(exists(select 1 from premium_subscriptions ps where ps.user_id=u.id and ps.active=true and ps.current_period_until>now()) or exists(select 1 from bot_admins a where a.platform_user_id=u.platform_user_id))`}
	q:=fmt.Sprintf(`select id,platform_user_id,platform_chat_id,coalesce(platform_dialog_id,''),coalesce(profile_link,''),coalesce(contact_phone,''),coalesce(username,''),coalesce(name,''),coalesce(gender,''),coalesce(preferred_gender,''),coalesce(flow_state,''),is_premium,referrer_user_id,referral_contact_credits,referral_rewarded_at,status,restricted_until,coalesce(premium_offer_chat_id,''),coalesce(premium_offer_message_id,''),created_at,updated_at from users u where platform_user_id not like 'fake_circle_%%' and %s order by updated_at desc`,where)
	args:=[]any{};if limit>0{q+=" limit $1";args=append(args,limit)};rows,err:=r.db.Query(ctx,q,args...);if err!=nil{return nil,err};defer rows.Close();out:=[]models.User{};for rows.Next(){u,err:=scanUser(rows);if err!=nil{return nil,err};out=append(out,*u)};return out,rows.Err()
}

func (r *Repository) SavePushRun(ctx context.Context,kind string,total,ok,failed int)error{_,err:=r.db.Exec(ctx,`insert into admin_push_runs(kind,total,succeeded,failed) values($1,$2,$3,$4)`,kind,total,ok,failed);return err}
func (r *Repository) AdminPushDiagnostics(ctx context.Context)(AdminPushStats,error){var s AdminPushStats;if err:=r.db.QueryRow(ctx,`select count(*) from users where platform_user_id not like 'fake_circle_%'`).Scan(&s.Users);err!=nil{return s,err};if err:=r.db.QueryRow(ctx,`select count(*) from users u where not exists(select 1 from premium_subscriptions ps where ps.user_id=u.id and ps.active=true and ps.current_period_until>now()) and u.platform_user_id not like 'fake_circle_%'`).Scan(&s.Leads);err!=nil{return s,err};if err:=r.db.QueryRow(ctx,`select count(*) from premium_subscriptions where active=true and current_period_until>now()`).Scan(&s.Active);err!=nil{return s,err};var t time.Time;err:=r.db.QueryRow(ctx,`select kind,total,succeeded,failed,created_at from admin_push_runs order by id desc limit 1`).Scan(&s.Kind,&s.Total,&s.Succeeded,&s.Failed,&t);if err==nil{s.CreatedAt=&t}else{s.Kind=""};return s,nil}
