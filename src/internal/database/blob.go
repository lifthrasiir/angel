package database

import (
	"context"
	"crypto/sha512"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"

	. "github.com/lifthrasiir/angel/internal/types"
)

// SaveBlob saves a blob to the blobs table. This function ensures the blob data exists.
// It sets ref_count = 0 for new blobs, and triggers will manage counting when messages are saved.
// It returns the SHA-512/256 hash of the data.
func SaveBlob(ctx context.Context, db SessionDbOrTx, data []byte) (string, error) {
	hasher := sha512.New512_256()
	hasher.Write(data)
	hash := hasher.Sum(nil)
	hashStr := hex.EncodeToString(hash)

	// Insert blob with ref_count = 0 if it doesn't exist
	// Triggers will increment ref_count when the message is actually saved
	_, err := db.ExecContext(ctx, `
		INSERT INTO S.blobs (id, data, ref_count) VALUES (?, ?, 0)
		ON CONFLICT(id) DO UPDATE SET
			data = excluded.data
	`, hashStr, data)
	if err != nil {
		return "", fmt.Errorf("failed to save blob: %w", err)
	}

	return hashStr, nil
}

// GetBlob retrieves a blob from the blobs table by its SHA-512/256 hash.
func GetBlob(db *SessionDatabase, hashStr string) ([]byte, error) {
	var data []byte
	err := db.QueryRow("SELECT data FROM S.blobs WHERE id = ?", hashStr).Scan(&data)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("blob not found for hash: %s", hashStr)
		}
		return nil, fmt.Errorf("failed to get blob: %w", err)
	}
	return data, nil
}

// GetBlobAsFileAttachment retrieves a blob and returns it as a FileAttachment with detected MIME type and appropriate filename.
func GetBlobAsFileAttachment(db *SessionDatabase, hash string) (FileAttachment, error) {
	// Get blob data
	blobData, err := GetBlob(db, hash)
	if err != nil {
		return FileAttachment{}, fmt.Errorf("failed to retrieve blob for hash %s: %w", hash, err)
	}

	// Determine MIME type by detecting content type
	mimeType := http.DetectContentType(blobData)

	// Generate filename with extension based on MIME type
	var filename string
	switch {
	case mimeType == "image/jpeg":
		filename = hash + ".jpg"
	case mimeType == "image/png":
		filename = hash + ".png"
	case mimeType == "image/gif":
		filename = hash + ".gif"
	case mimeType == "image/webp":
		filename = hash + ".webp"
	default:
		filename = hash // No extension if MIME type is not recognized
	}

	return FileAttachment{
		Hash:     hash,
		MimeType: mimeType,
		FileName: filename,
	}, nil
}
