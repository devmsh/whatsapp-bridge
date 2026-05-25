package db

// Schema is the complete DDL for the bridge database.
// Every table maps to a whatsmeow type. Column names are snake_case
// versions of the Go struct field names.
const Schema = `
CREATE TABLE IF NOT EXISTS messages (
    id                TEXT    NOT NULL,
    chat_jid          TEXT    NOT NULL,
    sender            TEXT    NOT NULL,
    sender_name       TEXT    NOT NULL DEFAULT '',
    push_name         TEXT    NOT NULL DEFAULT '',
    content           TEXT    NOT NULL DEFAULT '',
    timestamp         INTEGER NOT NULL,
    is_from_me        INTEGER NOT NULL DEFAULT 0,
    is_group          INTEGER NOT NULL DEFAULT 0,
    message_type      TEXT    NOT NULL DEFAULT '',
    device_id         TEXT    NOT NULL DEFAULT '',
    is_ephemeral      INTEGER NOT NULL DEFAULT 0,
    is_view_once      INTEGER NOT NULL DEFAULT 0,
    is_forwarded      INTEGER NOT NULL DEFAULT 0,
    forward_score     INTEGER NOT NULL DEFAULT 0,
    is_edit           INTEGER NOT NULL DEFAULT 0,
    edit_timestamp    INTEGER NOT NULL DEFAULT 0,
    original_id       TEXT    NOT NULL DEFAULT '',
    is_deleted        INTEGER NOT NULL DEFAULT 0,
    deleted_at        INTEGER NOT NULL DEFAULT 0,
    deleted_by        TEXT    NOT NULL DEFAULT '',
    media_type        TEXT    NOT NULL DEFAULT '',
    media_path        TEXT    NOT NULL DEFAULT '',
    media_mime        TEXT    NOT NULL DEFAULT '',
    media_size        INTEGER NOT NULL DEFAULT 0,
    media_caption     TEXT    NOT NULL DEFAULT '',
    media_filename    TEXT    NOT NULL DEFAULT '',
    thumbnail_path    TEXT    NOT NULL DEFAULT '',
    reply_to_id       TEXT    NOT NULL DEFAULT '',
    reply_to_sender   TEXT    NOT NULL DEFAULT '',
    reply_to_content  TEXT    NOT NULL DEFAULT '',
    mentions          TEXT    NOT NULL DEFAULT '',
    latitude          REAL    NOT NULL DEFAULT 0,
    longitude         REAL    NOT NULL DEFAULT 0,
    location_name     TEXT    NOT NULL DEFAULT '',
    location_address  TEXT    NOT NULL DEFAULT '',
    vcard_name        TEXT    NOT NULL DEFAULT '',
    vcard_data        TEXT    NOT NULL DEFAULT '',
    poll_id           TEXT    NOT NULL DEFAULT '',
    sticker_pack      TEXT    NOT NULL DEFAULT '',
    broadcast_list_jid TEXT   NOT NULL DEFAULT '',
    PRIMARY KEY (id, chat_jid)
);
CREATE INDEX IF NOT EXISTS idx_messages_chat_ts ON messages(chat_jid, timestamp);
CREATE INDEX IF NOT EXISTS idx_messages_sender ON messages(sender);
CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp);

CREATE TABLE IF NOT EXISTS chats (
    jid                TEXT    PRIMARY KEY,
    name               TEXT    NOT NULL DEFAULT '',
    chat_type          TEXT    NOT NULL DEFAULT '',
    last_message_at    INTEGER NOT NULL DEFAULT 0,
    unread_count       INTEGER NOT NULL DEFAULT 0,
    is_archived        INTEGER NOT NULL DEFAULT 0,
    is_pinned          INTEGER NOT NULL DEFAULT 0,
    is_muted           INTEGER NOT NULL DEFAULT 0,
    muted_until        INTEGER NOT NULL DEFAULT 0,
    disappearing_timer INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_chats_last_msg ON chats(last_message_at);

CREATE TABLE IF NOT EXISTS contacts (
    jid            TEXT    PRIMARY KEY,
    lid            TEXT    NOT NULL DEFAULT '',
    phone          TEXT    NOT NULL DEFAULT '',
    name           TEXT    NOT NULL DEFAULT '',
    push_name      TEXT    NOT NULL DEFAULT '',
    business_name  TEXT    NOT NULL DEFAULT '',
    verified_name  TEXT    NOT NULL DEFAULT '',
    is_business    INTEGER NOT NULL DEFAULT 0,
    status_text    TEXT    NOT NULL DEFAULT '',
    status_set_at  INTEGER NOT NULL DEFAULT 0,
    picture_id     TEXT    NOT NULL DEFAULT '',
    picture_url    TEXT    NOT NULL DEFAULT '',
    first_seen     INTEGER NOT NULL DEFAULT 0,
    last_seen      INTEGER NOT NULL DEFAULT 0,
    updated_at     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_contacts_lid ON contacts(lid);
CREATE INDEX IF NOT EXISTS idx_contacts_phone ON contacts(phone);

CREATE TABLE IF NOT EXISTS groups (
    jid                            TEXT    PRIMARY KEY,
    owner_jid                      TEXT    NOT NULL DEFAULT '',
    name                           TEXT    NOT NULL DEFAULT '',
    name_set_at                    INTEGER NOT NULL DEFAULT 0,
    name_set_by                    TEXT    NOT NULL DEFAULT '',
    topic                          TEXT    NOT NULL DEFAULT '',
    topic_id                       TEXT    NOT NULL DEFAULT '',
    topic_set_at                   INTEGER NOT NULL DEFAULT 0,
    topic_set_by                   TEXT    NOT NULL DEFAULT '',
    topic_deleted                  INTEGER NOT NULL DEFAULT 0,
    is_locked                      INTEGER NOT NULL DEFAULT 0,
    is_announce                    INTEGER NOT NULL DEFAULT 0,
    announce_version_id            TEXT    NOT NULL DEFAULT '',
    is_ephemeral                   INTEGER NOT NULL DEFAULT 0,
    disappearing_timer             INTEGER NOT NULL DEFAULT 0,
    is_incognito                   INTEGER NOT NULL DEFAULT 0,
    is_parent                      INTEGER NOT NULL DEFAULT 0,
    default_membership_approval_mode TEXT  NOT NULL DEFAULT '',
    linked_parent_jid              TEXT    NOT NULL DEFAULT '',
    is_default_sub                 INTEGER NOT NULL DEFAULT 0,
    member_add_mode                TEXT    NOT NULL DEFAULT '',
    join_approval_required         INTEGER NOT NULL DEFAULT 0,
    group_created                  INTEGER NOT NULL DEFAULT 0,
    creator_country_code           TEXT    NOT NULL DEFAULT '',
    participant_count              INTEGER NOT NULL DEFAULT 0,
    suspended                      INTEGER NOT NULL DEFAULT 0,
    updated_at                     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS group_participants (
    group_jid      TEXT    NOT NULL,
    jid            TEXT    NOT NULL,
    phone          TEXT    NOT NULL DEFAULT '',
    lid            TEXT    NOT NULL DEFAULT '',
    is_admin       INTEGER NOT NULL DEFAULT 0,
    is_super_admin INTEGER NOT NULL DEFAULT 0,
    display_name   TEXT    NOT NULL DEFAULT '',
    error_code     INTEGER NOT NULL DEFAULT 0,
    updated_at     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (group_jid, jid)
);
CREATE INDEX IF NOT EXISTS idx_gp_group ON group_participants(group_jid);

CREATE TABLE IF NOT EXISTS reactions (
    message_id  TEXT    NOT NULL,
    chat_jid    TEXT    NOT NULL,
    sender      TEXT    NOT NULL,
    sender_name TEXT    NOT NULL DEFAULT '',
    emoji       TEXT    NOT NULL DEFAULT '',
    timestamp   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (message_id, chat_jid, sender)
);
CREATE INDEX IF NOT EXISTS idx_reactions_msg ON reactions(message_id, chat_jid);

CREATE TABLE IF NOT EXISTS receipts (
    message_id   TEXT    NOT NULL,
    chat_jid     TEXT    NOT NULL,
    sender_jid   TEXT    NOT NULL,
    receipt_type TEXT    NOT NULL DEFAULT '',
    timestamp    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (message_id, chat_jid, sender_jid, receipt_type)
);
CREATE INDEX IF NOT EXISTS idx_receipts_msg ON receipts(message_id, chat_jid);

CREATE TABLE IF NOT EXISTS calls (
    call_id         TEXT    PRIMARY KEY,
    from_jid        TEXT    NOT NULL DEFAULT '',
    timestamp       INTEGER NOT NULL DEFAULT 0,
    call_creator    TEXT    NOT NULL DEFAULT '',
    group_jid       TEXT    NOT NULL DEFAULT '',
    event_type      TEXT    NOT NULL DEFAULT '',
    remote_platform TEXT    NOT NULL DEFAULT '',
    remote_version  TEXT    NOT NULL DEFAULT '',
    data            TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_calls_ts ON calls(timestamp);

CREATE TABLE IF NOT EXISTS newsletters (
    jid                TEXT    PRIMARY KEY,
    name               TEXT    NOT NULL DEFAULT '',
    description        TEXT    NOT NULL DEFAULT '',
    subscriber_count   INTEGER NOT NULL DEFAULT 0,
    verification_state TEXT    NOT NULL DEFAULT '',
    picture_id         TEXT    NOT NULL DEFAULT '',
    picture_url        TEXT    NOT NULL DEFAULT '',
    invite_code        TEXT    NOT NULL DEFAULT '',
    role               TEXT    NOT NULL DEFAULT '',
    muted              TEXT    NOT NULL DEFAULT '',
    state              TEXT    NOT NULL DEFAULT '',
    creation_time      INTEGER NOT NULL DEFAULT 0,
    updated_at         INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS polls (
    message_id     TEXT    NOT NULL,
    chat_jid       TEXT    NOT NULL,
    question       TEXT    NOT NULL DEFAULT '',
    options        TEXT    NOT NULL DEFAULT '',
    max_selections INTEGER NOT NULL DEFAULT 0,
    created_at     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (message_id, chat_jid)
);

CREATE TABLE IF NOT EXISTS poll_votes (
    poll_message_id  TEXT    NOT NULL,
    poll_chat_jid    TEXT    NOT NULL,
    voter_jid        TEXT    NOT NULL,
    selected_options TEXT    NOT NULL DEFAULT '',
    timestamp        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (poll_message_id, poll_chat_jid, voter_jid)
);

CREATE TABLE IF NOT EXISTS events_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT    NOT NULL DEFAULT '',
    jid        TEXT    NOT NULL DEFAULT '',
    actor_jid  TEXT    NOT NULL DEFAULT '',
    data       TEXT    NOT NULL DEFAULT '',
    timestamp  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_events_ts ON events_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_events_jid ON events_log(jid);

CREATE TABLE IF NOT EXISTS presence_cache (
    jid        TEXT    PRIMARY KEY,
    status     TEXT    NOT NULL DEFAULT '',
    last_seen  INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS sync_state (
    key        TEXT    PRIMARY KEY,
    value      TEXT    NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL DEFAULT 0
);

-- Circles: user-defined clusters of groups, contacts, and other circles.
-- Membership is many-to-many and circles may nest (cycles are rejected in code).
CREATE TABLE IF NOT EXISTS circles (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    color      TEXT    NOT NULL DEFAULT '',
    notes      TEXT    NOT NULL DEFAULT '',
    keywords   TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS circle_members (
    circle_id   INTEGER NOT NULL,
    member_type TEXT    NOT NULL,            -- 'group' | 'contact' | 'circle'
    member_ref  TEXT    NOT NULL,            -- JID for group/contact; circle id for circle
    added_at    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (circle_id, member_type, member_ref),
    FOREIGN KEY (circle_id) REFERENCES circles(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_circle_members_ref ON circle_members(member_type, member_ref);

-- Tags: user-defined labels (company, position, anything) for contacts.
CREATE TABLE IF NOT EXISTS tags (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL UNIQUE,
    color      TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS contact_tags (
    contact_jid TEXT    NOT NULL,
    tag_id      INTEGER NOT NULL,
    added_at    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (contact_jid, tag_id),
    FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_contact_tags_jid ON contact_tags(contact_jid);

-- Tasks: work items built on top of WhatsApp content. A task can span multiple
-- chats/groups (origin in one, completion in another) via task_messages.
CREATE TABLE IF NOT EXISTS tasks (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    title             TEXT    NOT NULL,
    description       TEXT    NOT NULL DEFAULT '',
    status            TEXT    NOT NULL DEFAULT 'open',   -- open | in_progress | done | cancelled
    priority          TEXT    NOT NULL DEFAULT 'normal', -- low | normal | high
    assignee_jid      TEXT    NOT NULL DEFAULT '',
    creator_jid       TEXT    NOT NULL DEFAULT '',
    due_at            INTEGER NOT NULL DEFAULT 0,
    completed_at      INTEGER NOT NULL DEFAULT 0,
    origin_chat_jid   TEXT    NOT NULL DEFAULT '',
    origin_message_id TEXT    NOT NULL DEFAULT '',
    review_status     TEXT    NOT NULL DEFAULT 'accepted', -- pending_review | accepted | rejected
    parent_id         INTEGER DEFAULT NULL REFERENCES tasks(id) ON DELETE SET NULL,
    created_at        INTEGER NOT NULL DEFAULT 0,
    updated_at        INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_assignee ON tasks(assignee_jid);
-- review_status index is created by the migration in db.go after the ALTER.

-- task_messages links a task to messages across any chats (cross-chat tasks).
CREATE TABLE IF NOT EXISTS task_messages (
    task_id    INTEGER NOT NULL,
    chat_jid   TEXT    NOT NULL,
    message_id TEXT    NOT NULL DEFAULT '',
    role       TEXT    NOT NULL DEFAULT 'related', -- origin | completion | comment | attachment | related
    added_at   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (task_id, chat_jid, message_id, role),
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_task_messages_task ON task_messages(task_id);
CREATE INDEX IF NOT EXISTS idx_task_messages_chat ON task_messages(chat_jid);
CREATE INDEX IF NOT EXISTS idx_task_messages_msg ON task_messages(message_id, chat_jid);

-- task_circles pins a task to circles.
CREATE TABLE IF NOT EXISTS task_circles (
    task_id   INTEGER NOT NULL,
    circle_id INTEGER NOT NULL,
    added_at  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (task_id, circle_id),
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
    FOREIGN KEY (circle_id) REFERENCES circles(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_task_circles_circle ON task_circles(circle_id);

-- entity_profiles: an AI-written (and user-editable) "purpose" description for
-- each circle, group, and contact/DM. Used as context for task extraction.
-- Refreshed on a 7-working-day cadence; a manual edit pins source='manual'.
CREATE TABLE IF NOT EXISTS entity_profiles (
    entity_type        TEXT    NOT NULL,            -- 'circle' | 'group' | 'contact'
    entity_ref         TEXT    NOT NULL,            -- JID for group/contact; circle id for circle
    description        TEXT    NOT NULL DEFAULT '',
    source             TEXT    NOT NULL DEFAULT 'auto', -- 'auto' | 'manual'
    msg_count_at_gen   INTEGER NOT NULL DEFAULT 0,   -- message count when last generated (staleness by activity)
    status             TEXT    NOT NULL DEFAULT 'pending', -- 'pending' | 'ok' | 'empty' | 'error'
    error              TEXT    NOT NULL DEFAULT '',
    generated_at       INTEGER NOT NULL DEFAULT 0,
    updated_at         INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (entity_type, entity_ref)
);
CREATE INDEX IF NOT EXISTS idx_profiles_generated ON entity_profiles(generated_at);
CREATE INDEX IF NOT EXISTS idx_profiles_status ON entity_profiles(status);

-- hidden_chats: chats the user has explicitly hidden. They are FILTERED OUT
-- of every AI feature unconditionally (extraction, profiling, briefing, draft
-- replies, search, dashboards) and from UI lists unless the request carries a
-- valid unlock token. AI never processes hidden chats — not even when unlocked.
CREATE TABLE IF NOT EXISTS hidden_chats (
    chat_jid TEXT    PRIMARY KEY,
    added_at INTEGER NOT NULL DEFAULT 0
);

-- media_understanding: AI-derived text for media messages.
-- One row per (message, kind). Used to enrich extractions and the UI:
--   kind='transcript'  — voice notes / audio, via whisper-cli (local).
--   kind='description' — images, via Claude vision.
CREATE TABLE IF NOT EXISTS media_understanding (
    chat_jid     TEXT    NOT NULL,
    message_id   TEXT    NOT NULL,
    kind         TEXT    NOT NULL,            -- 'transcript' | 'description'
    content      TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL DEFAULT 'pending', -- 'pending' | 'ok' | 'error' | 'skipped'
    error        TEXT    NOT NULL DEFAULT '',
    generated_at INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (chat_jid, message_id, kind),
    FOREIGN KEY (chat_jid, message_id) REFERENCES messages(chat_jid, id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_mu_status ON media_understanding(status);

-- briefings: AI-written daily digests (today / overdue / signal chats /
-- awaiting reply). One row per generated briefing. The data column is the
-- full JSON blob that the UI renders directly.
CREATE TABLE IF NOT EXISTS briefings (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    for_date      TEXT    NOT NULL,            -- YYYY-MM-DD (local)
    data          TEXT    NOT NULL DEFAULT '', -- JSON
    generated_at  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_briefings_date ON briefings(for_date);

-- chat_extraction_state: per-chat watermark for INCREMENTAL task extraction.
-- After a successful extraction sweep, last_msg_ts is set to the chat's max
-- message timestamp at that moment. The next run starts at last_msg_ts and
-- only sees new messages.
CREATE TABLE IF NOT EXISTS chat_extraction_state (
    chat_jid        TEXT    PRIMARY KEY,
    last_msg_ts     INTEGER NOT NULL DEFAULT 0,
    last_session_id TEXT    NOT NULL DEFAULT '',
    updated_at      INTEGER NOT NULL DEFAULT 0
);
`
