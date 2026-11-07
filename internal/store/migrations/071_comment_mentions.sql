-- 071_comment_mentions.sql: mentioning people in comments, with <@id>
-- (#224, docs/comments.md#mentions).
--
-- comment_mentions are the people each comment has mentioned, by their
-- user ID: one row for each, made when the comment, or an edit of it,
-- first mentions them, and kept if an edit takes the mention out, so that
-- no one is told twice. notified_at is when they were dealt with: emailed,
-- or passed over because they could not see what the comment is on, or
-- had turned mentions off. Each is claimed by setting it, so servers
-- sharing the database send it once. comment_id names a comment in any of
-- the comment tables (069), so it has no foreign key, as user_favorites'
-- object_id has none; a row goes with its person.
--
-- users.mention_emails is whether a person is emailed when mentioned: on
-- until they turn it off under Your account.

ALTER TABLE users ADD COLUMN mention_emails BOOLEAN NOT NULL DEFAULT TRUE;

CREATE TABLE comment_mentions (
    comment_id VARCHAR(36) NOT NULL,
    user_id VARCHAR(36) NOT NULL,
    mentioned_at TIMESTAMP NOT NULL,
    notified_at TIMESTAMP NULL,
    PRIMARY KEY (comment_id, user_id),
    CONSTRAINT fk_comment_mentions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_comment_mentions_due ON comment_mentions (notified_at, mentioned_at);
