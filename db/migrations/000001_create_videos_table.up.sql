CREATE TYPE video_status AS ENUM (
    'PENDING',
    'UPLOADED',
    'TRANSCODING',
    'COMPLETED',
    'FAILED'
);

CREATE TABLE IF NOT EXISTS videos (
    id VARCHAR(36) PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    original_file_name VARCHAR(255) NOT NULL,
    original_size BIGINT NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    status video_status NOT NULL DEFAULT 'PENDING',
    source_url TEXT,
    master_playlist_url TEXT,
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_vidoes_user_id ON videos(user_id);
CREATE INDEX IF NOT EXISTS idx_vidoes_status ON videos(status);
