package wa

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// MediaInfo holds metadata about a downloaded media file.
type MediaInfo struct {
	Type          string `json:"type"`
	Path          string `json:"path"`
	Mime          string `json:"mime"`
	Size          int    `json:"size"`
	Caption       string `json:"caption"`
	Filename      string `json:"filename"`
	ThumbnailPath string `json:"thumbnail_path,omitempty"`
}

type mediaDesc struct {
	mediaType string
	subdir    string
	download  whatsmeow.DownloadableMessage
	mime      string
	caption   string
	filename  string
	size      uint64
	thumbnail []byte
}

// DownloadMedia inspects a waE2E.Message and downloads any media it contains,
// subject to the given policy. Returns nil if no media is present. When the
// policy skips a file (disabled type or over the size cap), it returns
// metadata only (no Path) so the message still records that media exists.
func DownloadMedia(wa *whatsmeow.Client, msg *waE2E.Message, msgID, baseDir string, policy MediaPolicy, log waLog.Logger) *MediaInfo {
	desc := detectMedia(msg)
	if desc == nil {
		return nil
	}

	metaOnly := &MediaInfo{Type: desc.mediaType, Mime: desc.mime, Size: int(desc.size),
		Caption: desc.caption, Filename: desc.filename}

	if !policy.allows(desc.mediaType) {
		log.Debugf("Media type %s disabled by policy, recording metadata only", desc.mediaType)
		return metaOnly
	}
	if cap := policy.maxBytes(); cap > 0 && desc.size > cap {
		log.Infof("Media %s over size cap (%d bytes), recording metadata only", desc.mediaType, desc.size)
		return metaOnly
	}

	dir := filepath.Join(baseDir, desc.subdir)

	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Warnf("Failed to create media dir %s: %v", dir, err)
		return nil
	}

	ext := extensionFromMime(desc.mime)
	if desc.filename != "" {
		if idx := strings.LastIndex(desc.filename, "."); idx >= 0 {
			ext = desc.filename[idx:]
		}
	}
	filePath := filepath.Join(dir, msgID+ext)

	// Idempotent: skip if already downloaded
	if _, err := os.Stat(filePath); err == nil {
		return &MediaInfo{Type: desc.mediaType, Path: filePath, Mime: desc.mime,
			Size: int(desc.size), Caption: desc.caption, Filename: desc.filename}
	}

	data, err := wa.Download(context.Background(), desc.download)
	if err != nil {
		log.Warnf("Failed to download %s: %v", desc.mediaType, err)
		return &MediaInfo{Type: desc.mediaType, Mime: desc.mime, Size: int(desc.size),
			Caption: desc.caption, Filename: desc.filename}
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		log.Warnf("Failed to save %s: %v", filePath, err)
		return nil
	}

	info := &MediaInfo{
		Type: desc.mediaType, Path: filePath, Mime: desc.mime,
		Size: len(data), Caption: desc.caption, Filename: desc.filename,
	}

	// Download thumbnail if available
	if len(desc.thumbnail) > 0 {
		thumbDir := filepath.Join(baseDir, "thumbnails")
		os.MkdirAll(thumbDir, 0755)
		thumbPath := filepath.Join(thumbDir, msgID+".jpg")
		if err := os.WriteFile(thumbPath, desc.thumbnail, 0644); err == nil {
			info.ThumbnailPath = thumbPath
		}
	}

	log.Infof("Saved %s: %s (%d bytes)", desc.mediaType, filePath, len(data))
	return info
}

func detectMedia(msg *waE2E.Message) *mediaDesc {
	if msg == nil {
		return nil
	}
	if img := msg.GetImageMessage(); img != nil {
		return &mediaDesc{
			mediaType: "image", subdir: "images", download: img,
			mime: img.GetMimetype(), caption: img.GetCaption(),
			size: img.GetFileLength(), thumbnail: img.GetJPEGThumbnail(),
		}
	}
	if vid := msg.GetVideoMessage(); vid != nil {
		return &mediaDesc{
			mediaType: "video", subdir: "videos", download: vid,
			mime: vid.GetMimetype(), caption: vid.GetCaption(),
			size: vid.GetFileLength(), thumbnail: vid.GetJPEGThumbnail(),
		}
	}
	if aud := msg.GetAudioMessage(); aud != nil {
		t := "audio"
		if aud.GetPTT() {
			t = "voice_note"
		}
		return &mediaDesc{
			mediaType: t, subdir: "audio", download: aud,
			mime: aud.GetMimetype(), size: aud.GetFileLength(),
		}
	}
	if doc := msg.GetDocumentMessage(); doc != nil {
		return &mediaDesc{
			mediaType: "document", subdir: "documents", download: doc,
			mime: doc.GetMimetype(), caption: doc.GetCaption(),
			filename: doc.GetFileName(), size: doc.GetFileLength(),
			thumbnail: doc.GetJPEGThumbnail(),
		}
	}
	if stk := msg.GetStickerMessage(); stk != nil {
		return &mediaDesc{
			mediaType: "sticker", subdir: "stickers", download: stk,
			mime: stk.GetMimetype(), size: stk.GetFileLength(),
		}
	}
	return nil
}

func extensionFromMime(mime string) string {
	switch {
	case strings.Contains(mime, "jpeg"), strings.Contains(mime, "jpg"):
		return ".jpg"
	case strings.Contains(mime, "png"):
		return ".png"
	case strings.Contains(mime, "gif"):
		return ".gif"
	case strings.Contains(mime, "webp"):
		return ".webp"
	case strings.Contains(mime, "mp4"):
		return ".mp4"
	case strings.Contains(mime, "quicktime"):
		return ".mov"
	case strings.Contains(mime, "webm"):
		return ".webm"
	case strings.Contains(mime, "ogg"):
		return ".ogg"
	case strings.Contains(mime, "mpeg"):
		return ".mp3"
	case strings.Contains(mime, "m4a"):
		return ".m4a"
	case strings.Contains(mime, "pdf"):
		return ".pdf"
	default:
		return ".bin"
	}
}

// ContentFromMedia builds a text representation for messages that are pure media.
func ContentFromMedia(info *MediaInfo) string {
	if info == nil {
		return ""
	}
	if info.Caption != "" {
		return info.Caption
	}
	display := info.Filename
	if display == "" && info.Path != "" {
		display = info.Path
	}
	if display == "" {
		display = info.Type
	}
	return fmt.Sprintf("[%s:%s]", info.Type, display)
}
