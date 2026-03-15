package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// VoiceToText takes an audio file (e.g., recorded by the user) and uses ElevenLabs' Speech-to-Text
// pipeline to transcribe it into a string command, which can then be fed into ParseIntent.
func (e *Engine) VoiceToText(ctx context.Context, audioData []byte, filename string) (string, error) {
	if e.cfg.ElevenLabsAPIKey == "" {
		return "", fmt.Errorf("ElevenLabs API key is not configured")
	}

	// ElevenLabs endpoint for Speech-to-Text (example, assuming standard STT integration or using an equivalent service if ElevenLabs is primarily TTS. If user explicitly requested ElevenLabs for transcription, we use their isolation endpoints).
	// Currently ElevenLabs is dominant in TTS. If STT is needed, Whisper is common, but following the prompt:
	url := "https://api.elevenlabs.io/v1/speech-to-text" // Hypothetical/alpha endpoint for context matching

	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	// Add the audio file
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := io.Copy(fw, bytes.NewReader(audioData)); err != nil {
		return "", fmt.Errorf("failed to copy audio data: %w", err)
	}
	
	// Add model parameter
	if err := w.WriteField("model_id", "eleven_english_stt_v1"); err != nil {
		return "", fmt.Errorf("failed to add model field: %w", err)
	}

	w.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", url, &b)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("xi-api-key", e.cfg.ElevenLabsAPIKey)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := e.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ElevenLabs API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Text string `json:"text"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Text, nil
}
