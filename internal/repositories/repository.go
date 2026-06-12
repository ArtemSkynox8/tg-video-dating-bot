package repositories

import (
	"context"
	"errors"
	"time"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type Repository struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetUserByPlatformID(ctx context.Context, platformUserID string) (*models.User, error) {
	row := r.db.QueryRow(ctx, `
		select id, platform_user_id, platform_chat_id, coalesce(platform_dialog_id, ''), coalesce(profile_link, ''), coalesce(contact_phone, ''), coalesce(username, ''),
		       coalesce(name, ''), coalesce(gender, ''), coalesce(preferred_gender, ''), coalesce(flow_state, ''), is_premium,
		       status, restricted_until, coalesce(premium_offer_chat_id, ''), coalesce(premium_offer_message_id, ''), created_at, updated_at
		from users where platform_user_id = $1`, platformUserID)
	return scanUser(row)
}

func (r *Repository) GetUserByID(ctx context.Context, userID int64) (*models.User, error) {
	row := r.db.QueryRow(ctx, `
		select id, platform_user_id, platform_chat_id, coalesce(platform_dialog_id, ''), coalesce(profile_link, ''), coalesce(contact_phone, ''), coalesce(username, ''),
		       coalesce(name, ''), coalesce(gender, ''), coalesce(preferred_gender, ''), coalesce(flow_state, ''), is_premium,
		       status, restricted_until, coalesce(premium_offer_chat_id, ''), coalesce(premium_offer_message_id, ''), created_at, updated_at
		from users where id = $1`, userID)
	return scanUser(row)
}

func (r *Repository) UpsertPlatformUser(ctx context.Context, user models.User) (*models.User, error) {
	row := r.db.QueryRow(ctx, `
		insert into users (platform_user_id, platform_chat_id, platform_dialog_id, profile_link, username, status)
		values ($1, $2, $3, nullif($4, ''), nullif($5, ''), 'active')
		on conflict (platform_user_id) do update set
			platform_chat_id = excluded.platform_chat_id,
			platform_dialog_id = coalesce(nullif(excluded.platform_dialog_id, ''), users.platform_dialog_id),
			profile_link = coalesce(excluded.profile_link, users.profile_link),
			username = coalesce(excluded.username, users.username),
			updated_at = now()
		returning id, platform_user_id, platform_chat_id, coalesce(platform_dialog_id, ''), coalesce(profile_link, ''), coalesce(contact_phone, ''), coalesce(username, ''),
		          coalesce(name, ''), coalesce(gender, ''), coalesce(preferred_gender, ''), coalesce(flow_state, ''), is_premium,
		          status, restricted_until, coalesce(premium_offer_chat_id, ''), coalesce(premium_offer_message_id, ''), created_at, updated_at`,
		user.PlatformUserID, user.PlatformChatID, user.PlatformDialogID, user.ProfileLink, user.Username)
	return scanUser(row)
}

func (r *Repository) UpdateProfile(ctx context.Context, userID int64, name, gender, preferredGender string) error {
	_, err := r.db.Exec(ctx, `
		update users set name = $2, gender = $3, preferred_gender = $4, updated_at = now()
		where id = $1`, userID, name, gender, preferredGender)
	return err
}

func (r *Repository) UpdateName(ctx context.Context, userID int64, name string) error {
	_, err := r.db.Exec(ctx, `update users set name = $2, updated_at = now() where id = $1`, userID, name)
	return err
}

func (r *Repository) UpdateGender(ctx context.Context, userID int64, gender string) error {
	_, err := r.db.Exec(ctx, `update users set gender = $2, updated_at = now() where id = $1`, userID, gender)
	return err
}

func (r *Repository) UpdatePreferredGender(ctx context.Context, userID int64, preferredGender string) error {
	_, err := r.db.Exec(ctx, `update users set preferred_gender = $2, updated_at = now() where id = $1`, userID, preferredGender)
	return err
}

func (r *Repository) UpdateProfileLink(ctx context.Context, userID int64, profileLink string) error {
	_, err := r.db.Exec(ctx, `update users set profile_link = nullif($2, ''), updated_at = now() where id = $1`, userID, profileLink)
	return err
}

func (r *Repository) UpdateContactPhone(ctx context.Context, userID int64, phone string) error {
	_, err := r.db.Exec(ctx, `update users set contact_phone = nullif($2, ''), updated_at = now() where id = $1`, userID, phone)
	return err
}

func (r *Repository) UpdatePremiumOfferMessage(ctx context.Context, userID int64, chatID, messageID string) error {
	_, err := r.db.Exec(ctx, `
		update users
		set premium_offer_chat_id = nullif($2, ''),
		    premium_offer_message_id = nullif($3, ''),
		    updated_at = now()
		where id = $1`, userID, chatID, messageID)
	return err
}

func (r *Repository) ClearPremiumOfferMessage(ctx context.Context, userID int64) error {
	_, err := r.db.Exec(ctx, `
		update users
		set premium_offer_chat_id = null,
		    premium_offer_message_id = null,
		    updated_at = now()
		where id = $1`, userID)
	return err
}

func (r *Repository) SetFlowState(ctx context.Context, userID int64, state string) error {
	_, err := r.db.Exec(ctx, `update users set flow_state = $2, updated_at = now() where id = $1`, userID, state)
	return err
}

func (r *Repository) ClearFlowState(ctx context.Context, userID int64) error {
	return r.SetFlowState(ctx, userID, "")
}

func (r *Repository) UpdatePlatformDialogID(ctx context.Context, userID int64, platformDialogID string) error {
	if platformDialogID == "" {
		return nil
	}
	_, err := r.db.Exec(ctx, `
		update users set platform_dialog_id = $2, updated_at = now()
		where id = $1`, userID, platformDialogID)
	return err
}

func (r *Repository) SaveVideo(ctx context.Context, userID int64, mediaID, storageURL string, duration int) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `update videos set is_active = false where user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		insert into videos (user_id, platform_media_id, storage_url, duration, is_active)
		values ($1, $2, nullif($3, ''), $4, true)`, userID, mediaID, storageURL, duration); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) SavePendingVideo(ctx context.Context, userID int64, mediaID, storageURL string, duration int) (int64, error) {
	var id int64
	err := r.db.QueryRow(ctx, `
		insert into videos (user_id, platform_media_id, storage_url, duration, is_active)
		values ($1, $2, nullif($3, ''), $4, false)
		returning id`, userID, mediaID, storageURL, duration).Scan(&id)
	return id, err
}

func (r *Repository) ActivateVideo(ctx context.Context, userID, videoID int64) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `update videos set is_active = false where user_id = $1`, userID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `update videos set is_active = true where id = $1 and user_id = $2`, videoID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

func (r *Repository) FindCandidate(ctx context.Context, viewerID int64) (*models.Candidate, error) {
	row := r.db.QueryRow(ctx, `
		with viewer as (
			select id, preferred_gender from users where id = $1
		),
		priority as (
			select pq.candidate_user_id, 0 as rank
			from priority_queue pq
			where pq.target_user_id = $1 and pq.expires_at > now()
			order by pq.created_at asc
			limit 3
		),
		pool as (
			select u.id as candidate_user_id, 1 as rank
			from users u
			where u.id <> $1
		)
		select v.id, v.user_id, v.platform_media_id, coalesce(v.storage_url, ''), v.duration, v.is_active, v.created_at,
		       u.id, u.platform_user_id, u.platform_chat_id, coalesce(u.platform_dialog_id, ''), coalesce(u.profile_link, ''), coalesce(u.contact_phone, ''), coalesce(u.username, ''),
		       coalesce(u.name, ''), coalesce(u.gender, ''), coalesce(u.preferred_gender, ''), coalesce(u.flow_state, ''), u.is_premium,
		       u.status, u.restricted_until, coalesce(u.premium_offer_chat_id, ''), coalesce(u.premium_offer_message_id, ''), u.created_at, u.updated_at
		from (
			select * from priority
			union all
			select * from pool
		) ranked
		join users u on u.id = ranked.candidate_user_id
		join videos v on v.user_id = u.id and v.is_active = true
		cross join viewer
		where u.status = 'active'
		  and (u.restricted_until is null or u.restricted_until < now())
		  and (viewer.preferred_gender = 'any' or viewer.preferred_gender = u.gender)
		  and not exists (select 1 from views where viewer_id = $1 and video_id = v.id)
		order by ranked.rank asc, u.is_premium desc, random()
		limit 1`, viewerID)
	var c models.Candidate
	if err := row.Scan(
		&c.ID, &c.UserID, &c.PlatformMediaID, &c.StorageURL, &c.Duration, &c.IsActive, &c.CreatedAt,
		&c.Owner.ID, &c.Owner.PlatformUserID, &c.Owner.PlatformChatID, &c.Owner.PlatformDialogID, &c.Owner.ProfileLink, &c.Owner.ContactPhone, &c.Owner.Username,
		&c.Owner.Name, &c.Owner.Gender, &c.Owner.PreferredGender, &c.Owner.FlowState, &c.Owner.IsPremium,
		&c.Owner.Status, &c.Owner.RestrictedUntil, &c.Owner.PremiumOfferChatID, &c.Owner.PremiumOfferMessageID, &c.Owner.CreatedAt, &c.Owner.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (r *Repository) GetActiveVideoByUser(ctx context.Context, userID int64) (*models.Video, error) {
	row := r.db.QueryRow(ctx, `
		select id, user_id, platform_media_id, coalesce(storage_url, ''), duration, is_active, created_at
		from videos
		where user_id = $1 and is_active = true
		order by created_at desc
		limit 1`, userID)
	var v models.Video
	if err := row.Scan(&v.ID, &v.UserID, &v.PlatformMediaID, &v.StorageURL, &v.Duration, &v.IsActive, &v.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &v, nil
}

func (r *Repository) CreateView(ctx context.Context, viewerID, videoID, viewedUserID int64, action string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		insert into views (viewer_id, video_id, viewed_user_id, action)
		values ($1, $2, $3, $4)
		on conflict (viewer_id, video_id) do nothing`, viewerID, videoID, viewedUserID, action); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		insert into user_action_logs (user_id, action, payload)
		values ($1, $2, jsonb_build_object('video_id', $3::bigint, 'viewed_user_id', $4::bigint))`,
		viewerID, action, videoID, viewedUserID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) CreateLike(ctx context.Context, fromUserID, toUserID int64) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		insert into likes (from_user_id, to_user_id)
		values ($1, $2)
		on conflict (from_user_id, to_user_id) do nothing`, fromUserID, toUserID)
	return tag.RowsAffected() > 0, err
}

func (r *Repository) HasReverseLike(ctx context.Context, fromUserID, toUserID int64) (bool, error) {
	var ok bool
	err := r.db.QueryRow(ctx, `
		select exists(select 1 from likes where from_user_id = $1 and to_user_id = $2)`,
		toUserID, fromUserID).Scan(&ok)
	return ok, err
}

func (r *Repository) CreateMatch(ctx context.Context, user1ID, user2ID int64) error {
	if user2ID < user1ID {
		user1ID, user2ID = user2ID, user1ID
	}
	_, err := r.db.Exec(ctx, `
		insert into matches (user1_id, user2_id)
		values ($1, $2)
		on conflict (user1_id, user2_id) do nothing`, user1ID, user2ID)
	return err
}

func (r *Repository) EnqueuePriority(ctx context.Context, targetUserID, candidateUserID int64) error {
	_, err := r.db.Exec(ctx, `
		insert into priority_queue (target_user_id, candidate_user_id, reason, expires_at)
		values ($1, $2, 'liked_by_candidate', now() + interval '7 days')
		on conflict (target_user_id, candidate_user_id) do update set
			expires_at = excluded.expires_at,
			created_at = now()`, targetUserID, candidateUserID)
	return err
}

func (r *Repository) CreateVideoReport(ctx context.Context, reporterID, videoID, reportedUserID int64, reason string) error {
	_, err := r.db.Exec(ctx, `
		insert into video_reports (reporter_id, video_id, reported_user_id, reason)
		values ($1, $2, $3, $4)
		on conflict (reporter_id, video_id) do nothing`, reporterID, videoID, reportedUserID, reason)
	return err
}

func (r *Repository) FindVisibleMatch(ctx context.Context, userID, otherUserID int64) (int64, error) {
	var matchID int64
	err := r.db.QueryRow(ctx, `
		select id from matches
		where ((user1_id = $1 and user2_id = $2 and hidden_by_user1 = false)
		    or (user2_id = $1 and user1_id = $2 and hidden_by_user2 = false))
		limit 1`, userID, otherUserID).Scan(&matchID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return matchID, nil
}

func (r *Repository) HideMatchForUser(ctx context.Context, userID, otherUserID int64) error {
	_, err := r.db.Exec(ctx, `
		update matches set
			hidden_by_user1 = case when user1_id = $1 then true else hidden_by_user1 end,
			hidden_by_user2 = case when user2_id = $1 then true else hidden_by_user2 end
		where (user1_id = $1 and user2_id = $2) or (user2_id = $1 and user1_id = $2)`,
		userID, otherUserID)
	return err
}

func (r *Repository) CreateUserReport(ctx context.Context, reporterID, reportedUserID, matchID int64, reason string) error {
	_, err := r.db.Exec(ctx, `
		insert into user_reports (reporter_id, reported_user_id, match_id, reason)
		values ($1, $2, $3, $4)
		on conflict (reporter_id, reported_user_id, match_id) do nothing`,
		reporterID, reportedUserID, matchID, reason)
	return err
}

func (r *Repository) ApplyUserReportRestrictions(ctx context.Context, reportedUserID int64) error {
	var uniqueAll, uniqueWeek int
	if err := r.db.QueryRow(ctx, `
		select count(distinct reporter_id)
		from (
			select reporter_id, reported_user_id, created_at from video_reports
			union all
			select reporter_id, reported_user_id, created_at from user_reports
		) reports where reported_user_id = $1`, reportedUserID).Scan(&uniqueAll); err != nil {
		return err
	}
	if err := r.db.QueryRow(ctx, `
		select count(distinct reporter_id)
		from (
			select reporter_id, reported_user_id, created_at from video_reports
			union all
			select reporter_id, reported_user_id, created_at from user_reports
		) reports where reported_user_id = $1 and created_at > now() - interval '7 days'`, reportedUserID).Scan(&uniqueWeek); err != nil {
		return err
	}
	return r.restrictByCounts(ctx, reportedUserID, uniqueAll, uniqueWeek)
}

func (r *Repository) ApplyReportRestrictions(ctx context.Context, reportedUserID int64) error {
	var uniqueAll, uniqueWeek int
	if err := r.db.QueryRow(ctx, `select count(distinct reporter_id) from video_reports where reported_user_id = $1`, reportedUserID).Scan(&uniqueAll); err != nil {
		return err
	}
	if err := r.db.QueryRow(ctx, `select count(distinct reporter_id) from video_reports where reported_user_id = $1 and created_at > now() - interval '7 days'`, reportedUserID).Scan(&uniqueWeek); err != nil {
		return err
	}
	return r.restrictByCounts(ctx, reportedUserID, uniqueAll, uniqueWeek)
}

func (r *Repository) ListVisibleMatches(ctx context.Context, userID int64) ([]models.User, error) {
	rows, err := r.db.Query(ctx, `
		select u.id, u.platform_user_id, u.platform_chat_id, coalesce(u.platform_dialog_id, ''), coalesce(u.profile_link, ''), coalesce(u.contact_phone, ''), coalesce(u.username, ''),
		       coalesce(u.name, ''), coalesce(u.gender, ''), coalesce(u.preferred_gender, ''), coalesce(u.flow_state, ''), u.is_premium,
		       u.status, u.restricted_until, coalesce(u.premium_offer_chat_id, ''), coalesce(u.premium_offer_message_id, ''), u.created_at, u.updated_at
		from matches m
		join users u on u.id = case when m.user1_id = $1 then m.user2_id else m.user1_id end
		where (m.user1_id = $1 and hidden_by_user1 = false)
		   or (m.user2_id = $1 and hidden_by_user2 = false)
		order by m.created_at desc`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []models.User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

func (r *Repository) LatestContactRequest(ctx context.Context, userID int64) (*models.User, error) {
	row := r.db.QueryRow(ctx, `
		select u.id, u.platform_user_id, u.platform_chat_id, coalesce(u.platform_dialog_id, ''), coalesce(u.profile_link, ''), coalesce(u.contact_phone, ''), coalesce(u.username, ''),
		       coalesce(u.name, ''), coalesce(u.gender, ''), coalesce(u.preferred_gender, ''), coalesce(u.flow_state, ''), u.is_premium,
		       u.status, u.restricted_until, coalesce(u.premium_offer_chat_id, ''), coalesce(u.premium_offer_message_id, ''), u.created_at, u.updated_at
		from views v
		join users u on u.id = v.viewed_user_id
		where v.viewer_id = $1 and v.action = 'like'
		order by v.created_at desc
		limit 1`, userID)
	return scanUser(row)
}

func (r *Repository) Stats(ctx context.Context) (map[string]int64, error) {
	stats := map[string]int64{}
	queries := map[string]string{
		"users": "select count(*) from users",
		"active_users": "select count(*) from users where status = 'active'",
		"videos": "select count(*) from videos",
		"likes": "select count(*) from likes",
		"matches": "select count(*) from matches",
		"reports": "select (select count(*) from video_reports) + (select count(*) from user_reports)",
		"premium_users": "select count(*) from users where is_premium = true",
	}
	for key, query := range queries {
		var value int64
		if err := r.db.QueryRow(ctx, query).Scan(&value); err != nil {
			return nil, err
		}
		stats[key] = value
	}
	return stats, nil
}

func (r *Repository) ListUsers(ctx context.Context, limit int) ([]models.User, error) {
	rows, err := r.db.Query(ctx, `
		select id, platform_user_id, platform_chat_id, coalesce(platform_dialog_id, ''), coalesce(profile_link, ''), coalesce(contact_phone, ''), coalesce(username, ''),
		       coalesce(name, ''), coalesce(gender, ''), coalesce(preferred_gender, ''), coalesce(flow_state, ''), is_premium,
		       status, restricted_until, coalesce(premium_offer_chat_id, ''), coalesce(premium_offer_message_id, ''), created_at, updated_at
		from users
		order by created_at desc
		limit $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []models.User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

func (r *Repository) CreatePremiumPayment(ctx context.Context, userID int64, amount, provider, status, externalID string) error {
	_, err := r.db.Exec(ctx, `
		insert into premium_payments (user_id, amount, provider, status, external_id)
		values ($1, $2::numeric, $3, $4, nullif($5, ''))`,
		userID, amount, provider, status, externalID)
	return err
}

func (r *Repository) LatestPremiumPayment(ctx context.Context, userID int64) (string, string, error) {
	var externalID, status string
	err := r.db.QueryRow(ctx, `
		select coalesce(external_id, ''), status
		from premium_payments
		where user_id = $1 and provider = 'yookassa'
		order by created_at desc
		limit 1`, userID).Scan(&externalID, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	return externalID, status, nil
}

func (r *Repository) UpdatePremiumPaymentStatus(ctx context.Context, externalID, status string) error {
	_, err := r.db.Exec(ctx, `update premium_payments set status = $2 where external_id = $1`, externalID, status)
	return err
}

func (r *Repository) SetPremium(ctx context.Context, userID int64) error {
	_, err := r.db.Exec(ctx, `update users set is_premium = true, updated_at = now() where id = $1`, userID)
	return err
}

func (r *Repository) SetUserStatus(ctx context.Context, userID int64, status string) error {
	_, err := r.db.Exec(ctx, `
		update users set status = $2, restricted_until = null, updated_at = now()
		where id = $1`, userID, status)
	return err
}

func (r *Repository) DeleteActiveVideo(ctx context.Context, userID int64) error {
	_, err := r.db.Exec(ctx, `update videos set is_active = false where user_id = $1 and is_active = true`, userID)
	return err
}

func (r *Repository) ResetUser(ctx context.Context, userID int64) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `delete from priority_queue where target_user_id = $1 or candidate_user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from user_reports where reporter_id = $1 or reported_user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from video_reports where reporter_id = $1 or reported_user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from matches where user1_id = $1 or user2_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from likes where from_user_id = $1 or to_user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from views where viewer_id = $1 or viewed_user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from premium_payments where user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from videos where user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update users set name = null, gender = null, preferred_gender = null,
			profile_link = null, contact_phone = null,
			flow_state = '', is_premium = false, status = 'active',
			restricted_until = null, updated_at = now()
		where id = $1`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) ClearAllData(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `
		truncate table
			user_action_logs,
			premium_payments,
			user_reports,
			video_reports,
			priority_queue,
			matches,
			likes,
			views,
			videos,
			users
		restart identity cascade`)
	return err
}

func (r *Repository) restrictByCounts(ctx context.Context, reportedUserID int64, uniqueAll, uniqueWeek int) error {
	var until *time.Time
	now := time.Now().UTC()
	if uniqueWeek >= 30 {
		t := now.Add(72 * time.Hour)
		until = &t
	} else if uniqueAll >= 10 {
		t := now.Add(24 * time.Hour)
		until = &t
	}
	if until == nil {
		return nil
	}
	_, err := r.db.Exec(ctx, `update users set status = 'restricted', restricted_until = $2, updated_at = now() where id = $1`, reportedUserID, until)
	return err
}

func scanUser(row pgx.Row) (*models.User, error) {
	var u models.User
	if err := row.Scan(&u.ID, &u.PlatformUserID, &u.PlatformChatID, &u.PlatformDialogID, &u.ProfileLink, &u.ContactPhone, &u.Username,
		&u.Name, &u.Gender, &u.PreferredGender, &u.FlowState, &u.IsPremium, &u.Status, &u.RestrictedUntil, &u.PremiumOfferChatID, &u.PremiumOfferMessageID,
		&u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}
