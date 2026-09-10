package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ruhamabek/vortex/internal/domain"
	"github.com/ruhamabek/vortex/internal/storage/postgres"
)


const testDBURL = "postgres://vortex:vortex_secret_password@localhost:5432/vortex_db?sslmode=disable"

func setupTestDB(t *testing.T) *pgxpool.Pool{
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, testDBURL)

	if err != nil {
		t.Fatalf("failed to connect to test pg db: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cleanupCancel()

		_,_ = pool.Exec(cleanupCtx, "TRUNCATE TABLE videos CASCADE;")
		pool.Close()
	})

	return pool
}

func TestPostgresVideoRepository_Lifecycle(t *testing.T){
	pool := setupTestDB(t)
	defer pool.Close()

	repo := postgres.NewVideoRepository(pool)
	ctx := context.Background()

	video, err := domain.NewVideo("inception trailer", "trailer.mp4", 5000000, "user-12")

	if err != nil {
		t.Fatalf("failed to create video domain entry: %v", err)
	}

	if err := repo.Save(ctx, video); err != nil {
		t.Fatalf("failed to save video to postgres: %v", err)
	}

	found, err := repo.FindByID(ctx, video.ID)

	if err != nil{
		t.Fatalf("failed to find saved video: %v", err)
	}

	if found.Title != video.Title{
		t.Errorf("expected title %v, got %v", video.Title, found.Title )
	}

	if found.Status != domain.VideoStatusPending {
			t.Errorf("expected status PENDING, got %s", found.Status)
	}

	if err := found.MarkUploaded("raw/user-12/trailer.mp4"); err != nil {
		    t.Fatalf("failed to transition video state: %v", err)
	}

	if err := repo.Update(ctx, found); err != nil {
		t.Fatalf("failed to update video in postgres: %v", err)
	}

	updated, err := repo.FindByID(ctx, video.ID)

	if err != nil {
		t.Fatalf("failed to re-query updated video: %v", err)
	}

	if updated.Status != domain.VideoStatusUploaded {
		t.Errorf("expected status UPLOADED, got %s", updated.Status)
	}

	if updated.SourceURL != "raw/user-12/trailer.mp4" {
		t.Errorf("expected source_url raw/user-41/trailer.mp4, got %s", updated.SourceURL)
	}

	_, err = repo.FindByID(ctx, "non-existent-uuid-999")
		if err != domain.ErrVideoNotFound {
			t.Errorf("expected ErrVideoNotFound, got %v", err)
		}

	list, err := repo.ListByUserID(ctx, "user-12", 10, 0)
		if err != nil {
			t.Fatalf("failed to list videos by user: %v", err)
		}
		
		if len(list) == 0 {
			t.Errorf("expected at least 1 video in user list, got 0")
		}

}