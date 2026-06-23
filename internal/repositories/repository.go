package repositories

import (
	"context"
	"errors"
	"time"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/ai"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ChatProfile struct {
	CharacterID      string
	FreeMessagesUsed int
	SpicyTeaserShown bool
}

var ErrNotFound = errors.New("not found")

type Repository struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetChatProfile(ctx context.Context, userID int64) (ChatProfile, error) {
	var p ChatProfile
	err := r.db.QueryRow(ctx, `select character_id, free_messages_used, spicy_teaser_shown from chat_profiles where user_id=$1`, userID).
		Scan(&p.CharacterID, &p.FreeMessagesUsed, &p.SpicyTeaserShown)
	if errors.Is(err, pgx.ErrNoRows) { return p, ErrNotFound }
	return p, err
}

func (r *Repository) SetCharacter(ctx context.Context, userID int64, characterID string) error {
	_, err := r.db.Exec(ctx, `insert into chat_profiles(user_id, character_id) values($1,$2)
		on conflict(user_id) do update set character_id=excluded.character_id, free_messages_used=0,
		spicy_teaser_shown=false, updated_at=now()`, userID, characterID)
	if err != nil { return err }
	_, err = r.db.Exec(ctx, `delete from chat_messages where user_id=$1`, userID)
	return err
}

func (r *Repository) AddChatMessage(ctx context.Context, userID int64, role, content string) error {
	_, err := r.db.Exec(ctx, `insert into chat_messages(user_id, role, content) values($1,$2,$3)`, userID, role, content)
	return err
}

func (r *Repository) RecentChatMessages(ctx context.Context, userID int64, limit int) ([]ai.Message, error) {
	rows, err := r.db.Query(ctx, `select role, content from (
		select id, role, content from chat_messages where user_id=$1 order by id desc limit $2
	) q order by id`, userID, limit)
	if err != nil { return nil, err }
	defer rows.Close()
	out := make([]ai.Message, 0, limit)
	for rows.Next() { var m ai.Message; if err := rows.Scan(&m.Role, &m.Content); err != nil { return nil, err }; out = append(out, m) }
	return out, rows.Err()
}

func (r *Repository) IncrementFreeMessages(ctx context.Context, userID int64) error {
	_, err := r.db.Exec(ctx, `update chat_profiles set free_messages_used=free_messages_used+1, updated_at=now() where user_id=$1`, userID)
	return err
}

func (r *Repository) MarkSpicyTeaserShown(ctx context.Context, userID int64) error {
	_, err := r.db.Exec(ctx, `update chat_profiles set spicy_teaser_shown=true, updated_at=now() where user_id=$1`, userID)
	return err
}

func (r *Repository) CharacterMediaToken(ctx context.Context, characterID string) (string, error) {
	var token string
	err := r.db.QueryRow(ctx, `select media_token from character_media where character_id=$1`, characterID).Scan(&token)
	if errors.Is(err, pgx.ErrNoRows) { return "", ErrNotFound }
	return token, err
}

func (r *Repository) SaveCharacterMediaToken(ctx context.Context, characterID, token string) error {
	_, err := r.db.Exec(ctx, `insert into character_media(character_id, media_token) values($1,$2)
		on conflict(character_id) do update set media_token=excluded.media_token, updated_at=now()`, characterID, token)
	return err
}

func (r *Repository) GetUserByPlatformID(ctx context.Context, platformUserID string) (*models.User, error) {
	row := r.db.QueryRow(ctx, `
		select id, platform_user_id, platform_chat_id, coalesce(platform_dialog_id, ''), coalesce(profile_link, ''), coalesce(contact_phone, ''), coalesce(username, ''),
		       coalesce(name, ''), coalesce(gender, ''), coalesce(preferred_gender, ''), coalesce(flow_state, ''), is_premium,
		       referrer_user_id, referral_contact_credits, referral_rewarded_at, status, restricted_until, coalesce(premium_offer_chat_id, ''), coalesce(premium_offer_message_id, ''), created_at, updated_at
		from users where platform_user_id = $1`, platformUserID)
	return scanUser(row)
}

func (r *Repository) GetUserByID(ctx context.Context, userID int64) (*models.User, error) {
	row := r.db.QueryRow(ctx, `
		select id, platform_user_id, platform_chat_id, coalesce(platform_dialog_id, ''), coalesce(profile_link, ''), coalesce(contact_phone, ''), coalesce(username, ''),
		       coalesce(name, ''), coalesce(gender, ''), coalesce(preferred_gender, ''), coalesce(flow_state, ''), is_premium,
		       referrer_user_id, referral_contact_credits, referral_rewarded_at, status, restricted_until, coalesce(premium_offer_chat_id, ''), coalesce(premium_offer_message_id, ''), created_at, updated_at
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
		          referrer_user_id, referral_contact_credits, referral_rewarded_at, status, restricted_until, coalesce(premium_offer_chat_id, ''), coalesce(premium_offer_message_id, ''), created_at, updated_at`,
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

func (r *Repository) UpsertFakeVideoUser(ctx context.Context, platformUserID, name, gender, profileLink, mediaID, storageURL string, duration int) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID int64
	err = tx.QueryRow(ctx, `
		insert into users (platform_user_id, platform_chat_id, platform_dialog_id, profile_link, username, name, gender, preferred_gender, flow_state, status)
		values ($1, $1, '', nullif($4, ''), $1, $2, $3, 'any', '', 'active')
		on conflict (platform_user_id) do update set
			name = excluded.name,
			gender = excluded.gender,
			profile_link = excluded.profile_link,
			preferred_gender = excluded.preferred_gender,
			status = 'active',
			updated_at = now()
		returning id`, platformUserID, name, gender, profileLink).Scan(&userID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `update videos set is_active = false where user_id = $1`, userID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		insert into videos (user_id, platform_media_id, storage_url, duration, is_active)
		values ($1, $2, nullif($3, ''), $4, true)`, userID, mediaID, storageURL, duration)
	if err != nil {
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
		       u.referrer_user_id, u.referral_contact_credits, u.referral_rewarded_at, u.status, u.restricted_until, coalesce(u.premium_offer_chat_id, ''), coalesce(u.premium_offer_message_id, ''), u.created_at, u.updated_at
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
		&c.Owner.ReferrerUserID, &c.Owner.ReferralContactCredits, &c.Owner.ReferralRewardedAt,
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
		on conflict (viewer_id, video_id) do update set
			action = case when excluded.action = 'contact' then excluded.action else views.action end,
			created_at = case when excluded.action = 'contact' then now() else views.created_at end`, viewerID, videoID, viewedUserID, action); err != nil {
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

func (r *Repository) CreatePremiumOpenedMatches(ctx context.Context, userID int64) error {
	_, err := r.db.Exec(ctx, `
		insert into matches (user1_id, user2_id)
		select least(v.viewer_id, v.viewed_user_id), greatest(v.viewer_id, v.viewed_user_id)
		from views v
		join users u on u.id = v.viewed_user_id
		where v.viewer_id = $1
		  and v.viewed_user_id <> v.viewer_id
		  and v.action = 'like'
		  and u.platform_user_id like 'fake_circle_%'
		on conflict (user1_id, user2_id) do nothing`, userID)
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

func (r *Repository) ResetBrowseViews(ctx context.Context, userID int64) error {
	_, err := r.db.Exec(ctx, `delete from views where viewer_id = $1 and action in ('next', 'like')`, userID)
	return err
}

func (r *Repository) SetAdTagIfEmpty(ctx context.Context, userID int64, tag string) error {
	_, err := r.db.Exec(ctx, `
		update users
		set ad_tag = nullif($2, ''), updated_at = now()
		where id = $1 and ad_tag is null`, userID, tag)
	return err
}

func (r *Repository) UserAdTag(ctx context.Context, userID int64) (string, error) {
	var tag string
	err := r.db.QueryRow(ctx, `select coalesce(ad_tag, '') from users where id = $1`, userID).Scan(&tag)
	return tag, err
}

func (r *Repository) CreateUserActionLog(ctx context.Context, userID int64, action string) error {
	_, err := r.db.Exec(ctx, `
		insert into user_action_logs (user_id, action, payload)
		values ($1, $2, '{}'::jsonb)`, userID, action)
	return err
}

func (r *Repository) CreateOfferReachedLog(ctx context.Context, userID int64, reason string) (string, string, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(ctx)
	var tag string
	if err := tx.QueryRow(ctx, `select coalesce(ad_tag, '') from users where id = $1`, userID).Scan(&tag); err != nil {
		return "", "", err
	}
	var previous int64
	if err := tx.QueryRow(ctx, `select count(*) from user_action_logs where user_id = $1 and action = 'offer_reached'`, userID).Scan(&previous); err != nil {
		return "", "", err
	}
	offerType := "new"
	if previous > 0 {
		offerType = "repeat"
	}
	if _, err := tx.Exec(ctx, `
		insert into user_action_logs (user_id, action, payload)
		values ($1, 'offer_reached', jsonb_build_object('tag', nullif($2::text, ''), 'reason', $3::text, 'type', $4::text))`,
		userID, tag, reason, offerType); err != nil {
		return "", "", err
	}
	return tag, offerType, tx.Commit(ctx)
}

func (r *Repository) AdStats(ctx context.Context, tag string) ([]models.AdStats, error) {
	rows, err := r.db.Query(ctx, `
		with user_stats as (
			select coalesce(nullif(ad_tag, ''), 'без метки') as tag, count(*) as users
			from users
			where ($1 = '' or coalesce(nullif(ad_tag, ''), 'без метки') = $1)
			  and platform_user_id not like 'fake_circle_%'
			group by 1
		),
		offer_stats as (
			select coalesce(nullif(u.ad_tag, ''), 'без метки') as tag, count(*) as offer
			from user_action_logs l
			join users u on u.id = l.user_id
			where l.action = 'offer_reached'
			  and ($1 = '' or coalesce(nullif(u.ad_tag, ''), 'без метки') = $1)
			  and u.platform_user_id not like 'fake_circle_%'
			group by 1
		),
		buyer_stats as (
			select coalesce(nullif(u.ad_tag, ''), 'без метки') as tag,
			       count(distinct p.user_id) as buyers,
			       coalesce(sum(p.amount), 0)::float8 as sum
			from premium_payments p
			join users u on u.id = p.user_id
			where p.status = 'succeeded'
			  and ($1 = '' or coalesce(nullif(u.ad_tag, ''), 'без метки') = $1)
			  and u.platform_user_id not like 'fake_circle_%'
			group by 1
		),
		video_stats as (
			select coalesce(nullif(u.ad_tag, ''), 'без метки') as tag, count(distinct l.user_id) as videos
			from user_action_logs l
			join users u on u.id = l.user_id
			where l.action = 'recorded_video'
			  and ($1 = '' or coalesce(nullif(u.ad_tag, ''), 'без метки') = $1)
			  and u.platform_user_id not like 'fake_circle_%'
			group by 1
		),
		contact_stats as (
			select coalesce(nullif(u.ad_tag, ''), 'без метки') as tag, count(distinct l.user_id) as contact
			from user_action_logs l
			join users u on u.id = l.user_id
			where l.action = 'shared_contact'
			  and ($1 = '' or coalesce(nullif(u.ad_tag, ''), 'без метки') = $1)
			  and u.platform_user_id not like 'fake_circle_%'
			group by 1
		)
		select us.tag, us.users, coalesce(os.offer, 0), coalesce(vs.videos, 0), coalesce(cs.contact, 0), coalesce(bs.buyers, 0), coalesce(bs.sum, 0)
		from user_stats us
		left join offer_stats os on os.tag = us.tag
		left join buyer_stats bs on bs.tag = us.tag
		left join video_stats vs on vs.tag = us.tag
		left join contact_stats cs on cs.tag = us.tag
		order by us.users desc, us.tag asc`, tag)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stats := []models.AdStats{}
	for rows.Next() {
		var item models.AdStats
		if err := rows.Scan(&item.Tag, &item.Users, &item.Offer, &item.Videos, &item.Contact, &item.Buyers, &item.Sum); err != nil {
			return nil, err
		}
		stats = append(stats, item)
	}
	return stats, rows.Err()
}

func (r *Repository) SetReferrer(ctx context.Context, userID, referrerUserID int64) error {
	if userID == 0 || referrerUserID == 0 || userID == referrerUserID {
		return nil
	}
	_, err := r.db.Exec(ctx, `
		update users
		set referrer_user_id = $2, updated_at = now()
		where id = $1
		  and referrer_user_id is null
		  and exists (select 1 from users referrer where referrer.id = $2)`, userID, referrerUserID)
	return err
}

func (r *Repository) CompleteReferralIfNeeded(ctx context.Context, userID int64) (*models.User, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var referrerID int64
	tag, err := tx.Exec(ctx, `
		update users
		set referral_rewarded_at = now(), updated_at = now()
		where id = $1 and referrer_user_id is not null and referral_rewarded_at is null`, userID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, nil
	}
	if err := tx.QueryRow(ctx, `select referrer_user_id from users where id = $1`, userID).Scan(&referrerID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		update users
		set referral_contact_credits = referral_contact_credits + 1, updated_at = now()
		where id = $1`, referrerID); err != nil {
		return nil, err
	}
	row := tx.QueryRow(ctx, `
		select id, platform_user_id, platform_chat_id, coalesce(platform_dialog_id, ''), coalesce(profile_link, ''), coalesce(contact_phone, ''), coalesce(username, ''),
		       coalesce(name, ''), coalesce(gender, ''), coalesce(preferred_gender, ''), coalesce(flow_state, ''), is_premium,
		       referrer_user_id, referral_contact_credits, referral_rewarded_at, status, restricted_until, coalesce(premium_offer_chat_id, ''), coalesce(premium_offer_message_id, ''), created_at, updated_at
		from users where id = $1`, referrerID)
	referrer, err := scanUser(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return referrer, nil
}

func (r *Repository) ConsumeReferralContactCredit(ctx context.Context, userID, openedUserID int64) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		update users
		set referral_contact_credits = referral_contact_credits - 1, updated_at = now()
		where id = $1 and referral_contact_credits > 0`, userID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `
		insert into referral_contact_opens (user_id, opened_user_id)
		values ($1, $2)
		on conflict (user_id, opened_user_id) do nothing`, userID, openedUserID); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (r *Repository) FindRandomReferralContact(ctx context.Context, userID int64) (*models.Candidate, error) {
	row := r.db.QueryRow(ctx, `
		select v.id, v.user_id, v.platform_media_id, coalesce(v.storage_url, ''), v.duration, v.is_active, v.created_at,
		       u.id, u.platform_user_id, u.platform_chat_id, coalesce(u.platform_dialog_id, ''), coalesce(u.profile_link, ''), coalesce(u.contact_phone, ''), coalesce(u.username, ''),
		       coalesce(u.name, ''), coalesce(u.gender, ''), coalesce(u.preferred_gender, ''), coalesce(u.flow_state, ''), u.is_premium,
		       u.referrer_user_id, u.referral_contact_credits, u.referral_rewarded_at, u.status, u.restricted_until, coalesce(u.premium_offer_chat_id, ''), coalesce(u.premium_offer_message_id, ''), u.created_at, u.updated_at
		from (
			select v.*
			from videos v
			join users owner on owner.id = v.user_id
			where v.is_active = true
			  and v.user_id <> $1
			  and owner.status = 'active'
			  and (owner.restricted_until is null or owner.restricted_until < now())
			order by v.created_at desc
			limit 10
		) v
		join users u on u.id = v.user_id
		where not exists (
			select 1 from referral_contact_opens opened
			where opened.user_id = $1 and opened.opened_user_id = u.id
		)
		order by random()
		limit 1`, userID)
	var c models.Candidate
	if err := row.Scan(
		&c.ID, &c.UserID, &c.PlatformMediaID, &c.StorageURL, &c.Duration, &c.IsActive, &c.CreatedAt,
		&c.Owner.ID, &c.Owner.PlatformUserID, &c.Owner.PlatformChatID, &c.Owner.PlatformDialogID, &c.Owner.ProfileLink, &c.Owner.ContactPhone, &c.Owner.Username,
		&c.Owner.Name, &c.Owner.Gender, &c.Owner.PreferredGender, &c.Owner.FlowState, &c.Owner.IsPremium,
		&c.Owner.ReferrerUserID, &c.Owner.ReferralContactCredits, &c.Owner.ReferralRewardedAt,
		&c.Owner.Status, &c.Owner.RestrictedUntil, &c.Owner.PremiumOfferChatID, &c.Owner.PremiumOfferMessageID, &c.Owner.CreatedAt, &c.Owner.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
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
		       u.referrer_user_id, u.referral_contact_credits, u.referral_rewarded_at, u.status, u.restricted_until, coalesce(u.premium_offer_chat_id, ''), coalesce(u.premium_offer_message_id, ''), u.created_at, u.updated_at
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
		       u.referrer_user_id, u.referral_contact_credits, u.referral_rewarded_at, u.status, u.restricted_until, coalesce(u.premium_offer_chat_id, ''), coalesce(u.premium_offer_message_id, ''), u.created_at, u.updated_at
		from views v
		join users u on u.id = v.viewed_user_id
		where v.viewer_id = $1
		  and v.action = 'like'
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
		       referrer_user_id, referral_contact_credits, referral_rewarded_at, status, restricted_until, coalesce(premium_offer_chat_id, ''), coalesce(premium_offer_message_id, ''), created_at, updated_at
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

func (r *Repository) CreatePremiumPayment(ctx context.Context, userID int64, amount, provider, status, externalID, plan string, periodDays int, paymentMethodID, reason string) error {
	if periodDays == 0 {
		periodDays = 7
	}
	if reason == "" {
		reason = "initial"
	}
	_, err := r.db.Exec(ctx, `
		insert into premium_payments (user_id, amount, provider, status, external_id, plan, period_days, payment_method_id, reason)
		values ($1, $2::numeric, $3, $4, nullif($5, ''), nullif($6, ''), $7, nullif($8, ''), $9)`,
		userID, amount, provider, status, externalID, plan, periodDays, paymentMethodID, reason)
	return err
}

func (r *Repository) LatestPremiumPayment(ctx context.Context, userID int64) (*models.PremiumPayment, error) {
	var payment models.PremiumPayment
	err := r.db.QueryRow(ctx, `
		select coalesce(external_id, ''), status, coalesce(plan, ''), amount::text, period_days, coalesce(payment_method_id, '')
		from premium_payments
		where user_id = $1 and provider = 'yookassa'
		order by created_at desc
		limit 1`, userID).Scan(&payment.ExternalID, &payment.Status, &payment.Plan, &payment.Amount, &payment.PeriodDays, &payment.PaymentMethodID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &payment, nil
}

func (r *Repository) GetPremiumPaymentByExternalID(ctx context.Context, externalID string) (*models.PremiumPayment, int64, error) {
	var payment models.PremiumPayment
	var userID int64
	err := r.db.QueryRow(ctx, `
		select user_id, coalesce(external_id, ''), status, coalesce(plan, ''), amount::text, period_days, coalesce(payment_method_id, '')
		from premium_payments
		where provider = 'yookassa' and external_id = $1
		order by created_at desc
		limit 1`, externalID).Scan(&userID, &payment.ExternalID, &payment.Status, &payment.Plan, &payment.Amount, &payment.PeriodDays, &payment.PaymentMethodID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, ErrNotFound
		}
		return nil, 0, err
	}
	return &payment, userID, nil
}

func (r *Repository) UpdatePremiumPaymentStatus(ctx context.Context, externalID, status, paymentMethodID string) error {
	_, err := r.db.Exec(ctx, `
		update premium_payments
		set status = $2,
		    payment_method_id = coalesce(nullif($3, ''), payment_method_id)
		where external_id = $1`, externalID, status, paymentMethodID)
	return err
}

func (r *Repository) SetPremium(ctx context.Context, userID int64) error {
	_, err := r.db.Exec(ctx, `update users set is_premium = true, updated_at = now() where id = $1`, userID)
	return err
}

func (r *Repository) SetPremiumSubscription(ctx context.Context, userID int64, plan, amount string, periodDays int, paymentMethodID string, until time.Time) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `update users set is_premium = true, updated_at = now() where id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		insert into premium_subscriptions (user_id, plan, amount, period_days, payment_method_id, active, current_period_until, next_charge_at, updated_at)
		values ($1, $2, $3::numeric, $4, nullif($5, ''), true, $6, $6, now())
		on conflict (user_id) do update set
			plan = excluded.plan,
			amount = excluded.amount,
			period_days = excluded.period_days,
			payment_method_id = coalesce(excluded.payment_method_id, premium_subscriptions.payment_method_id),
			active = true,
			current_period_until = excluded.current_period_until,
			next_charge_at = excluded.next_charge_at,
			updated_at = now()`,
		userID, plan, amount, periodDays, paymentMethodID, until); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) ListDuePremiumSubscriptions(ctx context.Context, limit int) ([]models.PremiumSubscription, error) {
	rows, err := r.db.Query(ctx, `
		select u.id, u.platform_user_id, u.platform_chat_id, coalesce(u.platform_dialog_id, ''), coalesce(u.profile_link, ''), coalesce(u.contact_phone, ''), coalesce(u.username, ''),
		       coalesce(u.name, ''), coalesce(u.gender, ''), coalesce(u.preferred_gender, ''), coalesce(u.flow_state, ''), u.is_premium,
		       u.referrer_user_id, u.referral_contact_credits, u.referral_rewarded_at, u.status, u.restricted_until, coalesce(u.premium_offer_chat_id, ''), coalesce(u.premium_offer_message_id, ''), u.created_at, u.updated_at,
		       ps.plan, ps.amount::text, ps.period_days, coalesce(ps.payment_method_id, ''), ps.current_period_until, ps.next_charge_at
		from premium_subscriptions ps
		join users u on u.id = ps.user_id
		where ps.active = true
		  and ps.payment_method_id is not null
		  and ps.next_charge_at <= now()
		order by ps.next_charge_at asc
		limit $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.PremiumSubscription{}
	for rows.Next() {
		var sub models.PremiumSubscription
		if err := rows.Scan(&sub.User.ID, &sub.User.PlatformUserID, &sub.User.PlatformChatID, &sub.User.PlatformDialogID, &sub.User.ProfileLink, &sub.User.ContactPhone, &sub.User.Username,
			&sub.User.Name, &sub.User.Gender, &sub.User.PreferredGender, &sub.User.FlowState, &sub.User.IsPremium,
			&sub.User.ReferrerUserID, &sub.User.ReferralContactCredits, &sub.User.ReferralRewardedAt,
			&sub.User.Status, &sub.User.RestrictedUntil, &sub.User.PremiumOfferChatID, &sub.User.PremiumOfferMessageID, &sub.User.CreatedAt, &sub.User.UpdatedAt,
			&sub.Plan, &sub.Amount, &sub.PeriodDays, &sub.PaymentMethodID, &sub.CurrentPeriodUntil, &sub.NextChargeAt); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

func (r *Repository) ActivePremiumSubscription(ctx context.Context, userID int64) (*models.PremiumSubscription, error) {
	var sub models.PremiumSubscription
	err := r.db.QueryRow(ctx, `
		select ps.plan, ps.amount::text, ps.period_days, coalesce(ps.payment_method_id, ''), ps.current_period_until, ps.next_charge_at
		from premium_subscriptions ps
		where ps.user_id = $1 and ps.active = true and ps.current_period_until > now()
		limit 1`, userID).Scan(&sub.Plan, &sub.Amount, &sub.PeriodDays, &sub.PaymentMethodID, &sub.CurrentPeriodUntil, &sub.NextChargeAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &sub, nil
}

func (r *Repository) PostponePremiumSubscription(ctx context.Context, userID int64, nextChargeAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		update premium_subscriptions
		set next_charge_at = $2, updated_at = now()
		where user_id = $1`, userID, nextChargeAt)
	return err
}

func (r *Repository) CancelPremiumAutorenew(ctx context.Context, userID int64) error {
	_, err := r.db.Exec(ctx, `
		update premium_subscriptions
		set payment_method_id = null, updated_at = now()
		where user_id = $1 and active = true`, userID)
	return err
}

func (r *Repository) DisablePremiumSubscription(ctx context.Context, userID int64) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `update users set is_premium = false, updated_at = now() where id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update premium_subscriptions
		set active = false, updated_at = now()
		where user_id = $1`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
	if _, err := tx.Exec(ctx, `delete from referral_contact_opens where user_id = $1 or opened_user_id = $1`, userID); err != nil {
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
	if _, err := tx.Exec(ctx, `delete from premium_subscriptions where user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from videos where user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update users set name = null, gender = null, preferred_gender = null,
			profile_link = null, contact_phone = null,
			flow_state = '', is_premium = false, referrer_user_id = null,
			referral_contact_credits = 0, referral_rewarded_at = null, status = 'active',
			restricted_until = null, updated_at = now()
		where id = $1`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) ClearAllData(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `
		truncate table
			chat_messages,
			chat_profiles,
			user_action_logs,
			premium_subscriptions,
			premium_payments,
			admin_error_logs,
			admin_push_runs,
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
		&u.Name, &u.Gender, &u.PreferredGender, &u.FlowState, &u.IsPremium,
		&u.ReferrerUserID, &u.ReferralContactCredits, &u.ReferralRewardedAt,
		&u.Status, &u.RestrictedUntil, &u.PremiumOfferChatID, &u.PremiumOfferMessageID,
		&u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}
