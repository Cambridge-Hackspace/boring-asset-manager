-- 082_displays.sql: e-paper displays driven as printers (#200,
-- docs/printing.md#displays). 080 and 081 are other branches'.
--
-- displays is what each display shows, one row for a printer whose driver
-- drives displays, made when it is first shown something:
--
--   - template_id and asset_id are the template and asset it shows, NULL
--     when it was shown a test print, or they were deleted;
--   - shown_by and shown_by_token are who last showed it something, a
--     user's or an API token's ID, as whom refreshes lay it out again;
--   - image, content_type and etag are a display that pulls: the image it
--     fetches, in base64, its media type, and the tag that says whether it
--     changed; fetched_at is when the display last fetched it;
--   - refresh_at is when a refresh asked for is due, NULL when none is.
--     Asking again before then changes nothing, so a burst of changes is
--     one refresh; each is claimed by clearing it, so servers sharing the
--     database refresh once.

CREATE TABLE displays (
    printer_id VARCHAR(36) PRIMARY KEY,
    template_id VARCHAR(36),
    asset_id VARCHAR(36),
    shown_by VARCHAR(36),
    shown_by_token BOOLEAN NOT NULL DEFAULT FALSE,
    shown_at TIMESTAMP,
    image TEXT,
    content_type VARCHAR(64),
    etag VARCHAR(64),
    fetched_at TIMESTAMP,
    refresh_at TIMESTAMP,
    CONSTRAINT fk_displays_printer FOREIGN KEY (printer_id) REFERENCES printers(id) ON DELETE CASCADE,
    CONSTRAINT fk_displays_template FOREIGN KEY (template_id) REFERENCES templates(id) ON DELETE SET NULL,
    CONSTRAINT fk_displays_asset FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE SET NULL
);

CREATE INDEX idx_displays_refresh ON displays (refresh_at);
CREATE INDEX idx_displays_asset ON displays (asset_id);

-- display_tokens are the tokens displays that pull fetch their image with:
-- each reaches only its own display's image, and is kept as the SHA-256 of
-- its secret, as API tokens are. Deleting one revokes it.

CREATE TABLE display_tokens (
    id VARCHAR(36) PRIMARY KEY,
    printer_id VARCHAR(36) NOT NULL,
    name VARCHAR(255) NOT NULL,
    token_hash VARCHAR(64) NOT NULL,
    created_by VARCHAR(36),
    created_at TIMESTAMP NOT NULL,
    last_used_at TIMESTAMP,
    CONSTRAINT uq_display_tokens_hash UNIQUE (token_hash),
    CONSTRAINT fk_display_tokens_printer FOREIGN KEY (printer_id) REFERENCES printers(id) ON DELETE CASCADE
);

CREATE INDEX idx_display_tokens_printer ON display_tokens (printer_id);

-- A hook's actions are what BAM! itself does when it fires, beside the
-- event it records (docs/hooks.md#actions), as a JSON list: NULL for none.
-- refresh_displays refreshes the displays showing the asset, or showing
-- what is in it.

ALTER TABLE asset_actions ADD COLUMN actions TEXT;
