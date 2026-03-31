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
`
