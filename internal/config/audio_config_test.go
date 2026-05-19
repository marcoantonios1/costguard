package config

import (
	"strings"
	"testing"
)

// TestValidateAudioConfig_MissingLocalTranscriptionURL verifies that startup
// fails with a clear error when AUDIO_TRANSCRIPTION_PROVIDER=local but
// AUDIO_TRANSCRIPTION_URL is not set.
func TestValidateAudioConfig_MissingLocalTranscriptionURL(t *testing.T) {
	cfg := AudioConfig{
		TranscriptionProvider: "local",
		TranscriptionURL:      "",
		TTSProvider:           "openai",
		TTSURL:                "",
	}
	err := ValidateAudioConfig(cfg)
	if err == nil {
		t.Fatal("expected validation error for local transcription without URL, got nil")
	}
	if !strings.Contains(err.Error(), "AUDIO_TRANSCRIPTION_URL") {
		t.Errorf("error should mention AUDIO_TRANSCRIPTION_URL, got: %s", err.Error())
	}
}

// TestValidateAudioConfig_MissingLocalTTSURL verifies that startup fails with
// a clear error when AUDIO_TTS_PROVIDER=local but AUDIO_TTS_URL is not set.
func TestValidateAudioConfig_MissingLocalTTSURL(t *testing.T) {
	cfg := AudioConfig{
		TranscriptionProvider: "openai",
		TranscriptionURL:      "",
		TTSProvider:           "local",
		TTSURL:                "",
	}
	err := ValidateAudioConfig(cfg)
	if err == nil {
		t.Fatal("expected validation error for local TTS without URL, got nil")
	}
	if !strings.Contains(err.Error(), "AUDIO_TTS_URL") {
		t.Errorf("error should mention AUDIO_TTS_URL, got: %s", err.Error())
	}
}

// TestValidateAudioConfig_InvalidTranscriptionProvider verifies that an
// unrecognised AUDIO_TRANSCRIPTION_PROVIDER value causes startup to fail and
// the error names the offending config key.
func TestValidateAudioConfig_InvalidTranscriptionProvider(t *testing.T) {
	cfg := AudioConfig{
		TranscriptionProvider: "unknown",
		TTSProvider:           "openai",
	}
	err := ValidateAudioConfig(cfg)
	if err == nil {
		t.Fatal("expected validation error for unknown transcription provider, got nil")
	}
	if !strings.Contains(err.Error(), "AUDIO_TRANSCRIPTION_PROVIDER") {
		t.Errorf("error should mention AUDIO_TRANSCRIPTION_PROVIDER, got: %s", err.Error())
	}
}

// TestValidateAudioConfig_InvalidTTSProvider verifies that an unrecognised
// AUDIO_TTS_PROVIDER value causes startup to fail and the error names the key.
func TestValidateAudioConfig_InvalidTTSProvider(t *testing.T) {
	cfg := AudioConfig{
		TranscriptionProvider: "openai",
		TTSProvider:           "unknown",
	}
	err := ValidateAudioConfig(cfg)
	if err == nil {
		t.Fatal("expected validation error for unknown TTS provider, got nil")
	}
	if !strings.Contains(err.Error(), "AUDIO_TTS_PROVIDER") {
		t.Errorf("error should mention AUDIO_TTS_PROVIDER, got: %s", err.Error())
	}
}

// TestValidateAudioConfig_ValidOpenAI verifies that the default openai config
// passes validation without any URLs.
func TestValidateAudioConfig_ValidOpenAI(t *testing.T) {
	cfg := AudioConfig{
		TranscriptionProvider: "openai",
		TTSProvider:           "openai",
	}
	if err := ValidateAudioConfig(cfg); err != nil {
		t.Errorf("unexpected error for valid openai config: %v", err)
	}
}

// TestValidateAudioConfig_ValidLocal verifies that local providers pass
// validation when both URLs are present.
func TestValidateAudioConfig_ValidLocal(t *testing.T) {
	cfg := AudioConfig{
		TranscriptionProvider: "local",
		TranscriptionURL:      "http://speaches:9000",
		TTSProvider:           "local",
		TTSURL:                "http://kokoro:8880",
	}
	if err := ValidateAudioConfig(cfg); err != nil {
		t.Errorf("unexpected error for valid local config: %v", err)
	}
}
