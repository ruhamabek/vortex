package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ruhamabek/vortex/internal/domain"
)


type VideoRepository struct{
	pool *pgxpool.Pool
}

func NewVideoRepository(pool *pgxpool.Pool) *VideoRepository{
	 return &VideoRepository{pool: pool}
}

func (r *VideoRepository) Save(ctx context.Context, v *domain.Video) error{
	query := `
	     INSERT INTO videos (
		 id, title, original_file_name, original_size, user_id, 
		 status, source_url, master_playlist_url, error_message, 
		 created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`

	_, err := r.pool.Exec(ctx, query, 
	       v.ID,
		   v.Title,
		   v.OriginalFileName,
		   v.OriginalSize,
		   v.UserID,
		   string(v.Status),
		   v.SourceURL,
		   v.MasterPlaylistURL,
		   v.ErrorMessage,
		   v.CreatedAt,
		   v.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to save video: %v", err)
	}

	return nil
}

func (r *VideoRepository) FindByID(ctx context.Context, id string)(*domain.Video, error){
	query := `
	   SELECT 
			id, title, original_file_name, original_size, user_id, 
			status, source_url, master_playlist_url, error_message, 
			created_at, updated_at
	    FROM videos
		WHERE id= $1 
 	`
	var v domain.Video
	var status string
	var sourceURL, masterPlaylistURL, errorMessage *string

	err := r.pool.QueryRow(ctx, query, id).Scan(
			&v.ID,
			&v.Title,
			&v.OriginalFileName,
			&v.OriginalSize,
			&v.UserID,
			&status,
			&sourceURL,
			&masterPlaylistURL,
			&errorMessage,
			&v.CreatedAt,
			&v.UpdatedAt,
		)
	
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			  return nil, domain.ErrVideoNotFound
		}

		return nil, fmt.Errorf("failed to query video by id: %v", err)
	}

	v.Status = domain.VideoStatus(status)
		if sourceURL != nil {
			v.SourceURL = *sourceURL
		}
		if masterPlaylistURL != nil {
			v.MasterPlaylistURL = *masterPlaylistURL
		}
		if errorMessage != nil {
			v.ErrorMessage = *errorMessage
		}

	return &v, nil
}

func (r *VideoRepository) Update(ctx context.Context, v *domain.Video) error{
	query := `
	  UPDATE videos
	  SET 
		title = $2,
		status = $3,
		source_url = $4,
		master_playlist_url = $5,
		error_message = $6,
		updated_at = $7
	  WHERE id = $1
	`
	result, err := r.pool.Exec(ctx, query,
			v.ID,
			v.Title,
			string(v.Status),
			v.SourceURL,
			v.MasterPlaylistURL,
			v.ErrorMessage,
			v.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("failed to update video: %w", err)
		}
		if result.RowsAffected() == 0 {
			return domain.ErrVideoNotFound
		}
		return nil
}

func (r *VideoRepository) ListByUserID(ctx context.Context, userID string, limit, offset int) ([]*domain.Video, error) {
	query := `
		SELECT 
			id, title, original_file_name, original_size, user_id, 
			status, source_url, master_playlist_url, error_message, 
			created_at, updated_at
		FROM videos
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.pool.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query user videos: %w", err)
	}
	defer rows.Close()
	var videos []*domain.Video
	for rows.Next() {
		var v domain.Video
		var status string
		var sourceURL, masterPlaylistURL, errorMessage *string
		if err := rows.Scan(
			&v.ID,
			&v.Title,
			&v.OriginalFileName,
			&v.OriginalSize,
			&v.UserID,
			&status,
			&sourceURL,
			&masterPlaylistURL,
			&errorMessage,
			&v.CreatedAt,
			&v.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan video row: %w", err)
		}
		v.Status = domain.VideoStatus(status)
		if sourceURL != nil {
			v.SourceURL = *sourceURL
		}
		if masterPlaylistURL != nil {
			v.MasterPlaylistURL = *masterPlaylistURL
		}
		if errorMessage != nil {
			v.ErrorMessage = *errorMessage
		}
		videos = append(videos, &v)
	}
	return videos, nil
}

