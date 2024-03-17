package provider

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"rpc_service/global"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// 用于身份认证的拦截器
type AuthService interface {
	Authenticate(credentials Credentials) error
	Intercept(credentials Credentials) error
}

type authService struct {
	config AuthConfig
}

// 验证方式
type AuthType string

const (
	// 无验证
	NoAuth AuthType = "noauth"
	// 基本身份验证
	Basic AuthType = "basic"
	// 令牌身份验证
	Token AuthType = "token"
	// TLS 客户端身份验证
	TLSClientCert AuthType = "tls-client-cert"
	// TLS 服务端身份验证
	TLSServerCert AuthType = "tls-server-cert"
	// TLS 双向身份验证
	TLSBidirectional AuthType = "tls-bidirectional"
	// 自定义身份验证
	Custom AuthType = "custom"
	//JWT
	JWT AuthType = "jwt"
)

// 用于Jwt身份认证的凭证
type JwtAuthentication struct {
	// 用于Jwt身份认证的凭证
	key []byte

	ServiceAppID string
}

// AuthConfig 表示身份验证的配置
type AuthConfig struct {
	// TLSConfig 保存用于安全通信的 TLS 配置
	TLSConfig *tls.Config

	// Token 是身份验证令牌
	Token string

	// Username 和 Password 表示基本身份验证的凭据
	Username string
	Password string

	DecryptionConfig DecryptionConfig

	// 是否使用安全的传输协议
	RequireTransportSecurity bool

	// 验证方式
	AuthType AuthType

	// 用于Jwt身份认证的凭证
	JwtAuthentication *JwtAuthentication

	// 其他与身份验证机制相关的字段
	// ...
}

type DecryptionConfig struct {
	EncryptionKey     string // 加密密钥
	IV                string // IV 向量
	EncryptionKeyPath string // 加密密钥文件路径
	IVPath            string // IV 向量文件路径
}

type Credentials struct {
	AuthConfig *AuthConfig

	ExpiryTime time.Time

	// 添加其他必要字段
	// ...
}

type AuthError error

var (
	ErrAuthenticationFailed      AuthError = errors.New("身份认证失败")
	ErrMissingToken              AuthError = errors.New("身份验证失败：缺少令牌")
	ErrTokenExpired              AuthError = errors.New("身份验证失败：令牌已过期")
	ErrInvalidUsernameOrPassword AuthError = errors.New("身份验证失败：无效的用户名或密码")
	ErrFailedToGetStoredPassword AuthError = errors.New("从 Redis 获取存储密码时出错")
	ErrFailedToGetExpirationTime AuthError = errors.New("从 Redis 获取过期时间时出错")
	ErrWithOutEncryptionKeyIv    AuthError = errors.New("缺少加密密钥或 IV 向量")
	ErrMissingMetadata           AuthError = errors.New("缺少元数据")
	ErrJWT                       AuthError = errors.New("JWT 认证失败")
)

// Intercept 实现拦截器的 Intercept 方法
func (au *authService) Intercept(credentials Credentials) error {
	// 进行身份认证
	err := au.Authenticate(credentials)
	if err != nil {
		fmt.Errorf("身份认证失败: %v", err)
		return err
	}

	// 调用下一个拦截器或处理函数
	return nil
}
func NewAuthService(config AuthConfig) AuthService {
	return &authService{
		config: config,
	}
}

// Authenticate 实现拦截器的 Authenticate 方法
// 接受者改成protocol.RPCMsg
func (au *authService) Authenticate(credentials Credentials) error {
	// 使用提供的凭据执行身份验证逻辑
	// ...

	// 示例：检查令牌是否有效且未过期
	if credentials.AuthConfig.Token == "" {
		return ErrAuthenticationFailed
	}

	// 示例：检查令牌是否过期
	if !credentials.ExpiryTime.IsZero() && time.Now().After(credentials.ExpiryTime) {
		return ErrMissingToken
	}

	// 从配置文件检查用户名和密码是否有效
	if credentials.AuthConfig.Username != "" && credentials.AuthConfig.Password != "" {
		// 执行用户名和密码验证逻辑

		// 如果身份验证失败，则返回错误
		if !validUsernameAndPassword(credentials.AuthConfig) {
			// TODO:继续从数据库验证
			return ErrTokenExpired
		}
	}

	// 如果身份验证成功，则返回 nil
	return nil
}

// validUsernameAndPassword 函数用于验证用户名和密码
func validUsernameAndPassword(AuthConfig *AuthConfig) bool {
	// 执行验证逻辑，例如与数据库或其他身份验证机制进行比较

	username := AuthConfig.Username
	password := AuthConfig.Password
	decryptionConfig := AuthConfig.DecryptionConfig

	// 从 Redis 中获取存储的密码和过期时间
	passwordKey := fmt.Sprintf("password:%s", username)
	storedPassword, err := global.RedisClient.HGet(passwordKey, "password").Result()
	if err != nil {
		fmt.Printf("从 Redis 获取用户名 %s 的存储密码时出错：%v\n", username, err)
		return false
	}

	expiryTimeStr, err := global.RedisClient.HGet(passwordKey, "expiry_time").Result()
	if err != nil {
		fmt.Printf("从 Redis 获取用户名 %s 的过期时间时出错：%v\n", username, err)
		return false
	}

	decryptedPassword := ""

	// 认证方式
	switch {
	case AuthConfig.AuthType == Basic:
		decryptedPassword, err = decryptPassword(storedPassword, decryptionConfig.EncryptionKey, decryptionConfig.IV)
		if err != nil {
			fmt.Printf("解密用户名 %s 的存储密码时出错：%v\n", username, err)
			return false
		}
	case AuthConfig.AuthType == JWT:
		err = authenticateJWT(*AuthConfig)
		if err != nil {
			fmt.Printf("JWT 认证失败:%s , %v\n ", AuthConfig.JwtAuthentication.ServiceAppID, ErrJWT)
			return false
		}
	}

	// 检查密码是否过期
	expiryTime, err := time.Parse(time.RFC3339, expiryTimeStr)
	if err != nil {
		fmt.Printf("解析用户名 %s 的过期时间时出错：%v\n", username, err)
		return false
	}

	if time.Now().After(expiryTime) {
		fmt.Printf("用户名 %s 的密码已过期\n", username)
		return false
	}

	// 将提供的密码与解密后的密码进行比较
	if password != decryptedPassword {
		fmt.Printf("用户名 %s 的密码无效\n", username)
		return false
	}

	// 如果用户名和密码有效，则返回 true
	return true
}

func decryptPassword(encodedPassword string, encryptionKey string, iv string) (string, error) {

	// 解码经过 Base64 编码的密码
	encryptedData, err := base64.StdEncoding.DecodeString(encodedPassword)
	if err != nil {
		return "", err
	}

	// 将密钥和 IV 从字符串转换为字节数组
	key := []byte(encryptionKey)
	ivBytes := []byte(iv)

	// 初始化 AES 解密器
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	// 指定解密模式和 IV 向量
	decrypter := cipher.NewCBCDecrypter(block, ivBytes)

	// 创建解密后的字节数组
	decryptedData := make([]byte, len(encryptedData))

	// 解密操作
	decrypter.CryptBlocks(decryptedData, encryptedData)

	// 去除填充
	decryptedData, err = removePadding(decryptedData)
	if err != nil {
		return "", err
	}

	// 将解密后的字节数组转换为字符串
	decryptedPassword := string(decryptedData)

	return decryptedPassword, nil
}

// removePadding 函数用于移除解密后的填充
func removePadding(data []byte) ([]byte, error) {
	paddingLength := int(data[len(data)-1])
	if paddingLength > len(data) {
		return nil, errors.New("无效的填充")
	}
	return data[:len(data)-paddingLength], nil
}

// GetRequestMetadata 客户端JWt认证 token生成
func (a authService) GetRequestMetadata(ctx context.Context, uri ...string) (string, error) {
	// Create a new token object, specifying signing method and the claims
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		ID:        "example",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
	})

	// Sign and get the complete encoded token as a string using the secret
	tokenString, err := token.SignedString(a.config.JwtAuthentication.key)
	if err != nil {
		return "", err
	}

	return "authorization:JWT:" + tokenString, nil
}

// 服务端JWT认证
func authenticateJWT(config AuthConfig) error {
	// 从config中获取JWT令牌
	tokenString := config.Token
	if tokenString == "" {
		return ErrMissingToken
	}
	// 解析JWT令牌
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// 验证算法
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("无效的签名算法")
		}

		// 返回用于验证令牌的密钥
		return config.JwtAuthentication, nil
	})
	if err != nil {
		return err
	}

	// 验证令牌是否有效
	if !token.Valid {
		return ErrAuthenticationFailed
	}

	// 检查令牌是否过期
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return ErrAuthenticationFailed
	}

	expirationTime, ok := claims["exp"].(float64)
	if !ok {
		return ErrAuthenticationFailed
	}

	refreshThreshold, ok := claims["refresh"].(float64)
	if !ok {
		return ErrAuthenticationFailed
	}

	tokenRefreshDuration, ok := claims["duration"].(time.Duration)
	if !ok {
		return ErrAuthenticationFailed
	}
	ServiceAppID, ok := claims["duration"].(time.Duration)
	if !ok {
		return ErrAuthenticationFailed
	}
	// 获取当前时间
	currentTime := time.Now().Unix()

	// 校验过期时间
	if currentTime > int64(expirationTime) {
		return ErrTokenExpired
	}

	// 获取刷新过期时间
	refreshExpirationTime, ok := claims["refresh_exp"].(float64)
	if !ok {
		return ErrAuthenticationFailed
	}

	// 校验刷新过期时间
	if currentTime > int64(refreshExpirationTime) {
		// 刷新令牌过期，需要重新获取新的令牌
		return ErrTokenExpired
	}

	// 更新令牌的过期时间
	if currentTime > int64(expirationTime-refreshThreshold) {
		// 更新令牌的过期时间为当前时间加上刷新阈值
		newExpirationTime := time.Now().Add(tokenRefreshDuration).Unix()
		claims["exp"] = newExpirationTime

		// 重新签名令牌
		newToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		newTokenString, err := newToken.SignedString(config.JwtAuthentication)
		if err != nil {
			return err
		}

		// 更新config中的令牌值
		config.Token = newTokenString
	}

	// 在JWT令牌的声明中检查所需的服务应用ID
	if claims["serviceAppID"] != ServiceAppID {
		return ErrAuthenticationFailed
	}

	// 可以根据需要添加其他验证逻辑

	return nil
}
