CREATE TABLE files (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    slug         TEXT    NOT NULL,
    filename     TEXT    NOT NULL,
    version      INTEGER NOT NULL,
    size         INTEGER NOT NULL,
    sha256       TEXT    NOT NULL,
    content_type TEXT    NOT NULL,
    created_at   TEXT    NOT NULL,
    UNIQUE (slug, filename, version)
);

CREATE INDEX idx_files_slug ON files(slug);
