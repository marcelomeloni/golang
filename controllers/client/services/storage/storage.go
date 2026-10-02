package storage

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

var allowedMimeTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

const maxFileSizeBytes = 5 << 20 // 5 MB

type UploadResult struct {
	URL string
}

// UploadOrgImage faz upload de uma imagem para o Supabase Storage e retorna a URL pública.
// folder deve ser algo como "org-logos" ou "org-banners".
func UploadOrgImage(file multipart.File, header *multipart.FileHeader, folder string) (*UploadResult, error) {
	if err := validateImage(header); err != nil {
		return nil, err
	}

	data, err := io.ReadAll(io.LimitReader(file, maxFileSizeBytes+1))
	if err != nil {
		return nil, fmt.Errorf("erro ao ler arquivo: %w", err)
	}
	if int64(len(data)) > maxFileSizeBytes {
		return nil, fmt.Errorf("arquivo excede o limite de 5MB")
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	objectPath := fmt.Sprintf("%s/%s%s", folder, uuid.New().String(), ext)

	publicURL, err := uploadToSupabase(data, objectPath, header.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}

	return &UploadResult{URL: publicURL}, nil
}

func validateImage(header *multipart.FileHeader) error {
	contentType := header.Header.Get("Content-Type")
	if !allowedMimeTypes[contentType] {
		return fmt.Errorf("tipo de arquivo não permitido: %s", contentType)
	}
	if header.Size > maxFileSizeBytes {
		return fmt.Errorf("arquivo excede o limite de 5MB")
	}
	return nil
}

func uploadToSupabase(data []byte, objectPath, contentType string) (string, error) {
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_SERVICE_KEY")
	bucket := os.Getenv("STORAGE_BUCKET")

	uploadURL := fmt.Sprintf("%s/storage/v1/object/%s/%s", supabaseURL, bucket, objectPath)

	req, err := http.NewRequest(http.MethodPost, uploadURL, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("erro ao criar requisição de upload: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+supabaseKey)
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("erro ao enviar arquivo para storage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("storage retornou status %d: %s", resp.StatusCode, string(body))
	}

	publicURL := fmt.Sprintf("%s/storage/v1/object/public/%s/%s", supabaseURL, bucket, objectPath)
	return publicURL, nil
}