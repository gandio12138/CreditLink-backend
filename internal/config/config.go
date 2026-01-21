package config

import (
	"fmt"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	Chain    ChainConfig
	Signer   SignerConfig
	JWT      JWTConfig
}

type ServerConfig struct {
	Port string
	Mode string // debug, release, test
}

type DatabaseConfig struct {
	Host            string `mapstructure:"host"`
	Port            int    `mapstructure:"port"`
	User            string `mapstructure:"user"`
	Password        string `mapstructure:"password"`
	DBName          string `mapstructure:"dbname"`
	MaxIdleConns    int    `mapstructure:"max_idle_conns"`
	MaxOpenConns    int    `mapstructure:"max_open_conns"`
	ConnMaxLifetime int    `mapstructure:"conn_max_lifetime"` // minutes
}

// DSN returns PostgreSQL connection string
func (d *DatabaseConfig) DSN() string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=disable TimeZone=Asia/Shanghai",
		d.Host, d.User, d.Password, d.DBName, d.Port)
}

type JWTConfig struct {
	Secret     string `mapstructure:"secret"`
	ExpireHour int    `mapstructure:"expire_hour"`
}

type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type ChainConfig struct {
	RPCURL      string `mapstructure:"rpc_url"`
	ChainID     int64  `mapstructure:"chain_id"`
	LendingPool string `mapstructure:"lending_pool"`
	StartBlock  uint64 `mapstructure:"start_block"`
}

type SignerConfig struct {
	KeyProvider      string `mapstructure:"key_provider"`      // aws_kms, gcp_kms, local
	PrivateKey       string `mapstructure:"private_key"`       // only for local provider (dev only)
	KMSKeyID         string `mapstructure:"kms_key_id"`
	SignatureTTL     int    `mapstructure:"signature_ttl"`     // seconds
	RateLimitPerUser int    `mapstructure:"rate_limit_per_user"`
	RateLimitPerIP   int    `mapstructure:"rate_limit_per_ip"`
}

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./configs")
	viper.AddConfigPath(".")

	viper.AutomaticEnv()

	// Set defaults
	viper.SetDefault("server.port", "8080")
	viper.SetDefault("server.mode", "debug")
	viper.SetDefault("database.max_idle_conns", 10)
	viper.SetDefault("database.max_open_conns", 100)
	viper.SetDefault("database.conn_max_lifetime", 60)
	viper.SetDefault("signer.signature_ttl", 300)
	viper.SetDefault("signer.rate_limit_per_user", 5)
	viper.SetDefault("signer.rate_limit_per_ip", 20)
	viper.SetDefault("jwt.expire_hour", 24)

	if err := viper.ReadInConfig(); err != nil {
		// Config file not found is OK, we'll use defaults and env vars
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
