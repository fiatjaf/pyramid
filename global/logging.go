package global

import (
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

var Log zerolog.Logger

func InitLogging(dataPath string) error {
	path := filepath.Join(dataPath, "log")

	rotator := &lumberjack.Logger{
		Filename:   path,
		MaxSize:    10, // MB before rotation
		MaxBackups: 3,  // number of old files to keep
		MaxAge:     28, // days to keep old files
	}

	Log = zerolog.New(zerolog.MultiLevelWriter(
		zerolog.ConsoleWriter{Out: os.Stdout},
		rotator,
	)).With().Timestamp().Logger()

	return nil
}
