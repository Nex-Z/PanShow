package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	authCookieName   = "panshow_auth"
	accessCookieName = "panshow_access"
)

type Store struct {
	client *redis.Client
	ttl    time.Duration
}

func NewRedisStore(client *redis.Client, ttl time.Duration) *Store {
	return &Store{client: client, ttl: ttl}
}

func CookieName() string {
	return authCookieName
}

func AccessCookieName() string {
	return accessCookieName
}

func (s *Store) TTL() time.Duration {
	return s.ttl
}

func (s *Store) Create(ctx context.Context, userID uint) (string, error) {
	token := uuid.NewString()
	if err := s.client.Set(ctx, sessionKey(token), strconv.FormatUint(uint64(userID), 10), s.ttl).Err(); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) CreateAccessToken(ctx context.Context) (string, error) {
	token := uuid.NewString()
	if err := s.client.Set(ctx, accessSessionKey(token), "1", s.ttl).Err(); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) AccessTokenExists(ctx context.Context, token string) (bool, error) {
	if token == "" {
		return false, nil
	}
	exists, err := s.client.Exists(ctx, accessSessionKey(token)).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

func (s *Store) UserID(ctx context.Context, token string) (uint, error) {
	raw, err := s.client.Get(ctx, sessionKey(token)).Result()
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(value), nil
}

func (s *Store) Delete(ctx context.Context, token string) error {
	return s.client.Del(ctx, sessionKey(token)).Err()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *Store) MarkPasswordPassed(ctx context.Context, token, dir string, version uint) error {
	return s.client.Set(ctx, accessKey(token, dir, version), "1", s.ttl).Err()
}

func (s *Store) HasPasswordPassed(ctx context.Context, token, dir string, version uint) (bool, error) {
	value, err := s.client.Get(ctx, accessKey(token, dir, version)).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == "1", nil
}

func (s *Store) GetJSON(ctx context.Context, key string, dest any) (bool, error) {
	raw, err := s.client.Get(ctx, cacheKey(key)).Bytes()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error {
	if ttl <= 0 {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, cacheKey(key), raw, ttl).Err()
}

func (s *Store) DeleteCachePatterns(ctx context.Context, patterns ...string) error {
	for _, pattern := range patterns {
		iter := s.client.Scan(ctx, 0, cacheKey(pattern), 100).Iterator()
		keys := make([]string, 0, 100)
		for iter.Next(ctx) {
			keys = append(keys, iter.Val())
			if len(keys) >= 100 {
				if err := s.client.Del(ctx, keys...).Err(); err != nil {
					return err
				}
				keys = keys[:0]
			}
		}
		if err := iter.Err(); err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := s.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
	}
	return nil
}

func sessionKey(token string) string {
	return "panshow:session:" + token
}

func accessSessionKey(token string) string {
	return "panshow:access-session:" + token
}

func accessKey(token, dir string, version uint) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(dir))
	return fmt.Sprintf("panshow:access:%s:%s:%d", token, encoded, version)
}

func cacheKey(key string) string {
	return "panshow:cache:" + key
}
