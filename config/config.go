package config

import (
	"fmt"
	"log"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

const ConfigFilePath = "config/config.local.yaml"

type ServiceTarget struct {
	URL     string `mapstructure:"url"`
	Prefix  string `mapstructure:"prefix"`
	StripPrefix bool `mapstructure:"strip_prefix"`
}

type JWTConfig struct {
	SecretKey string `mapstructure:"secret_key"`
}

type ServerConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	ReadTimeout     time.Duration `mapstructure:"ReadTimeout"`
	WriteTimeout    time.Duration `mapstructure:"WriteTimeout"`
	IdleTimeout     time.Duration `mapstructure:"IdleTimeout"`
	ShutDownTimeOut time.Duration `mapstructure:"ShutDownTimeOut"`
}

type CORSConfig struct {
	AllowedOrigins []string `mapstructure:"allowed_origins"`
	AllowedMethods []string `mapstructure:"allowed_methods"`
	AllowedHeaders []string `mapstructure:"allowed_headers"`
	MaxAge         int      `mapstructure:"max_age"`
}

type LoggerConfig struct {
	Logger zap.Config `mapstructure:"logger"`
}

type Config struct {
	Server   ServerConfig             `mapstructure:"server"`
	JWT      JWTConfig                `mapstructure:"jwt"`
	CORS     CORSConfig               `mapstructure:"cors"`
	Services map[string]ServiceTarget `mapstructure:"services"`
	Logger   LoggerConfig             `mapstructure:"logger"`
	// Публичные пути — не требуют JWT
	PublicPaths []string `mapstructure:"public_paths"`
}

func LoadConfig() (Config, error) {
	v := viper.New()
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.SetEnvPrefix("APP")

	if cfgFile := os.Getenv("CONFIG_FILE_PATH"); cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		v.SetConfigFile(ConfigFilePath)
		log.Println("CONFIG_FILE_PATH not set, using default:", ConfigFilePath)
	}

	if err := v.ReadInConfig(); err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			zapLevelHook,
			stringToIntHook,
		),
		Result:  &cfg,
		TagName: "mapstructure",
	})
	if err != nil {
		return Config{}, err
	}
	if err := dec.Decode(v.AllSettings()); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func zapLevelHook(from reflect.Type, to reflect.Type, data interface{}) (interface{}, error) {
	if to != reflect.TypeOf(zap.AtomicLevel{}) {
		return data, nil
	}
	s, ok := data.(string)
	if !ok {
		return data, nil
	}
	var lvl zap.AtomicLevel
	if err := lvl.UnmarshalText([]byte(s)); err != nil {
		return nil, err
	}
	return lvl, nil
}

func stringToIntHook(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	if f.Kind() != reflect.String || t.Kind() != reflect.Int {
		return data, nil
	}
	return strconv.Atoi(data.(string))
}
