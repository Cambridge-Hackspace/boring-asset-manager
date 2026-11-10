-- 081_assistant.sql: an assistant beside the page, in the web app, using
-- BAM!'s own tools as the person signed in (#266, docs/assistant.md). 079
-- is #265's, and 080 is left for #213.
--
-- assistant_settings is the instance's provider and limits, in one row
-- whose id is 'instance'; with no row, every setting has its default.
-- Settings given in the configuration file or the environment take the
-- place of these, and show in Admin as set there:
--
--   - enabled turns the panel on; acknowledged_by and acknowledged_at are
--     who turned it on, having been told that what people type and what
--     the tools return go to the provider;
--   - provider is anthropic (the Messages API) or openai (any chat
--     completions API that takes tools), at endpoint, with model;
--   - api_key is written and never returned, as notification channels'
--     secrets are;
--   - name is what the assistant is called, in the panel and in history;
--   - max_tokens is the most a reply may take, max_steps the most calls to
--     the model one message may lead to, and max_tools the most tools
--     offered, 0 for every one the person may use;
--   - person_messages, person_tokens, instance_messages and
--     instance_tokens are the limits a day, 0 for none;
--   - keep_days is how long a conversation is kept after its last message.

CREATE TABLE assistant_settings (
    id VARCHAR(36) PRIMARY KEY,
    enabled BOOLEAN NOT NULL,
    provider VARCHAR(32) NOT NULL,
    endpoint VARCHAR(2048) NOT NULL,
    model VARCHAR(255) NOT NULL,
    api_key TEXT,
    name VARCHAR(255) NOT NULL,
    max_tokens INTEGER NOT NULL,
    max_steps INTEGER NOT NULL,
    max_tools INTEGER NOT NULL,
    person_messages INTEGER NOT NULL,
    person_tokens BIGINT NOT NULL,
    instance_messages INTEGER NOT NULL,
    instance_tokens BIGINT NOT NULL,
    keep_days INTEGER NOT NULL,
    acknowledged_by VARCHAR(36),
    acknowledged_at TIMESTAMP NULL,
    updated_by VARCHAR(36),
    updated_at TIMESTAMP NOT NULL
);

-- assistant_guides start conversations with a purpose. Each has an
-- address, as flows do, at /g/<slug>; the purpose in plain words; the
-- categories and link types it works with, by ID, as JSON lists; and the
-- questions it makes sure are answered, as a JSON list of text.

CREATE TABLE assistant_guides (
    id VARCHAR(36) PRIMARY KEY,
    slug VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    purpose TEXT NOT NULL,
    category_ids TEXT NOT NULL,
    link_type_ids TEXT NOT NULL,
    questions TEXT NOT NULL,
    enabled BOOLEAN NOT NULL,
    created_by VARCHAR(36),
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT uq_assistant_guides_slug UNIQUE (slug)
);

-- assistant_conversations are each person's own, readable by no one else,
-- admins included, and deleted keep_days after their last message. A
-- conversation a guide started names it, and keeps the answers to its
-- questions in progress, as JSON. pending_call is the tool call waiting for
-- the person to confirm it, if any. user_id is not a foreign key, so that
-- conversations can be restored apart from people; they are deleted when
-- the account is archived.

CREATE TABLE assistant_conversations (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL,
    title VARCHAR(255) NOT NULL,
    guide_id VARCHAR(36),
    progress TEXT,
    pending_call VARCHAR(255),
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX idx_assistant_conversations_user ON assistant_conversations (user_id, updated_at);
CREATE INDEX idx_assistant_conversations_updated ON assistant_conversations (updated_at);

-- assistant_messages are a conversation's messages, in order (seq): the
-- person's, the model's, and what the tools answered, each as JSON blocks
-- in content. A person's message keeps the page it was sent from in
-- context. raw is the model's reply as its provider gave it, raw_provider
-- which provider, so it is sent back unchanged.

CREATE TABLE assistant_messages (
    id VARCHAR(36) PRIMARY KEY,
    conversation_id VARCHAR(36) NOT NULL,
    seq INTEGER NOT NULL,
    role VARCHAR(16) NOT NULL,
    content TEXT NOT NULL,
    context TEXT,
    raw TEXT,
    raw_provider VARCHAR(32),
    tokens BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_assistant_messages_conversation FOREIGN KEY (conversation_id) REFERENCES assistant_conversations(id) ON DELETE CASCADE
);

CREATE INDEX idx_assistant_messages_conversation ON assistant_messages (conversation_id, seq);

-- assistant_usage counts each person's messages and tokens a day (UTC,
-- YYYY-MM-DD), against the limits; the instance's are their sum.

CREATE TABLE assistant_usage (
    user_id VARCHAR(36) NOT NULL,
    day VARCHAR(10) NOT NULL,
    messages INTEGER NOT NULL,
    tokens BIGINT NOT NULL,
    PRIMARY KEY (user_id, day)
);

-- assistant.use shows the panel; it is not scopable. It goes to the admin
-- role, which is granted every permission when roles are seeded.

INSERT INTO permissions (id, permission_key, description)
SELECT '3c7e1a9d-5b2f-4d8e-a6c4-9e1f3b5d7a20', 'assistant.use', 'Core permission: assistant.use'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'assistant.use');
