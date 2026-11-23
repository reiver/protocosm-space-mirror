package main

import (
	"codeberg.org/reiver/go-field"

	"protocosm-space-mirror/cfg"
	"protocosm-space-mirror/srv/log"
)

func showandtell() {
	log := logsrv.Begin()
	defer log.End()

	log.Highlight(
		field.S("CFG"),

		field.String("LOG_LEVEL", cfg.LogLevel()),
		field.String("DB_FILE_NAME", cfg.DBFileName()),
		field.String("BLOB_DIR", cfg.BlobDir()),
		field.String("INTERNAL_TOKENS_FILE", cfg.InternalTokensFileName()),
		field.String("SPACES_FILE", cfg.SpacesFileName()),
		field.Int("UNLIST_MAX", cfg.UnlistMax()),
		field.Duration("PURGE_GRACE", cfg.PurgeGrace()),
		field.Bool("--force-unlist", cfg.ForceUnlist()),

		field.Uint16("PUBLIC_PORT", cfg.HTTPPublicTcpPort()),
		field.String("INTERNAL_ADDR", cfg.HTTPInternalTcpAddress()),

		field.Duration("HTTP_HEADER_READ_TIMEOUT", cfg.HTTPHeaderReadTimeOut()),
		field.Duration("HTTP_IDLE_TIMEOUT", cfg.HTTPIdleTimeOut()),
		field.Duration("HTTP_SHUTDOWN_TIMEOUT", cfg.HTTPShutdownTimeOut()),
	)
}
