package jwt

// import (
// 	"time"

// 	"github.com/golang-jwt/jwt/v5"
// )

// const (
// 	ExpireDuration = 3600 * time.Second
// 	JwtSecretKey   = "abc123"
// )

// type MyClaims struct {
// 	Id       int64  `json:"id"`
// 	Username string `json:"username"`
// 	jwt.StandardClaims
// }

// // 生成token
// func GenerateToken(id int64, username string) (string, error) {
// 	// 定义token的过期时间
// 	expireTime := time.Now().Add(ExpireDuration).Unix()

// 	// 创建一个自定义的Claims
// 	myClaims := &MyClaims{
// 		Id:       id,
// 		Username: username,
// 		StandardClaims: jwt.StandardClaims{
// 			Audience:  "",
// 			ExpiresAt: expireTime,
// 			Id:        "",
// 			IssuedAt:  time.Now().Unix(),
// 			Issuer:    "lym",
// 			NotBefore: 0,
// 			Subject:   "",
// 		},
// 	}

// 	// 使用 JWT 签名算法生成token
// 	token := jwt.NewWithClaims(jwt.SigningMethodHS256, myClaims)

// 	// 将token进行加盐加密
// 	// 注意：该方法参数虽然是interface{}，但是必须传入[]byte类型的参数
// 	tokenString, err := token.SignedString([]byte(JwtSecretKey))
// 	if err != nil {
// 		return "", err
// 	}

// 	return tokenString, nil

// }

// func ParseToken(tokenString string) (*MyClaims, error) {
// 	// 解析 token
// 	token, err := jwt.ParseWithClaims(tokenString, &MyClaims{}, func(token *jwt.Token) (interface{}, error) {
// 		// 注意：虽然返回值是interface{}，但是这里必须返回[]byte类型，否则运行时会报错key is of invalid type
// 		return []byte(JwtSecretKey), nil
// 	})
// 	if err != nil {
// 		return nil, err
// 	}

// 	if myClaims, ok := token.Claims.(*MyClaims); ok && token.Valid {
// 		return myClaims, nil
// 	} else {
// 		return nil, jwt.NewValidationError("invalid token", jwt.ValidationErrorClaimsInvalid)
// 	}
// }
