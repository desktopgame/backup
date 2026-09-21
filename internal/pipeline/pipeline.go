// Package pipeline composes the tar -> zstd -> age stream.
package pipeline

import (
	"fmt"
	"io"

	"filippo.io/age"
	"github.com/klauspost/compress/zstd"
)

// Encrypt runs write (which receives a plaintext writer) through a zstd
// compressor and then an age encryptor, streaming the result to w.
func Encrypt(w io.Writer, recipients []age.Recipient, write func(io.Writer) error) error {
	if len(recipients) == 0 {
		return fmt.Errorf("no age recipients configured")
	}
	aw, err := age.Encrypt(w, recipients...)
	if err != nil {
		return fmt.Errorf("initializing age encryption: %w", err)
	}
	zw, err := zstd.NewWriter(aw, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		_ = aw.Close()
		return fmt.Errorf("initializing zstd compression: %w", err)
	}

	if err := write(zw); err != nil {
		_ = zw.Close()
		_ = aw.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		_ = aw.Close()
		return fmt.Errorf("finalizing compression: %w", err)
	}
	if err := aw.Close(); err != nil {
		return fmt.Errorf("finalizing encryption: %w", err)
	}
	return nil
}
