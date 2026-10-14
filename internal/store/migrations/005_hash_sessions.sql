-- 005_hash_sessions.sql: sessions are now stored by the SHA-256 hash of their
-- token, so a copy of the database cannot be used to sign in. Sessions
-- stored before this held the token itself and cannot be converted, so they
-- are ended and everyone signs in again once.

DELETE FROM sessions;
