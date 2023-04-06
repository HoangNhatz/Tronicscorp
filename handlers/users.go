package handlers

import (
	"Tronicsorp/config"
	"Tronicsorp/dbiface"
	"context"
	"net/http"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/golang-jwt/jwt"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	Email    string `json:"username" bson:"username" validate:"required,email"`
	Password string `json:"password,omitempty" bson:"password" validate:"required,min=8,max=300"`
}

type UserHandler struct {
	Col dbiface.CollectionAPI
}

type userValidator struct {
	validator *validator.Validate
}

func (u *userValidator) Validate(i interface{}) error {
	return u.validator.Struct(i)
}

func insertUser(ctx context.Context, user User, collection dbiface.CollectionAPI) (interface{}, error) {
	var newUser User
	res := collection.FindOne(ctx, bson.M{"username": user.Email})
	if err := res.Decode(&newUser); err != nil && err != mongo.ErrNoDocuments {
		log.Errorf("Unable to decode user: %v", err)
		return nil, echo.NewHTTPError(500, "Unable to decode user")
	}
	if newUser.Email != "" {
		log.Errorf("User by %s already exists", user.Email)
		return nil, echo.NewHTTPError(400, "User already exists")
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.Password), 8)
	if err != nil {
		log.Errorf("Unable to hash password: %v", err)
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "Unable to process the password")
	}
	user.Password = string(hashedPassword)
	insertRes, err := collection.InsertOne(ctx, user)
	if err != nil {
		log.Errorf("Unable to insert user: %v", err)
		return nil, echo.NewHTTPError(500, "Unable to insert user")
	}
	return insertRes.InsertedID, nil
}

// CreateUser creates a new user
func (u *UserHandler) CreateUser(c echo.Context) error {
	var user User
	c.Echo().Validator = &userValidator{validator: v}
	if err := c.Bind(&user); err != nil {
		log.Errorf("Unable to bind: %v", err)
		return err
	}
	if err := c.Validate(user); err != nil {
		log.Errorf("Unable to validate the %v: %v ", user, err)
		return err
	}
	userIDs, err := insertUser(context.Background(), user, u.Col)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, userIDs)
}

func (u *UserHandler) AuthUser(c echo.Context) error {
	var user User
	c.Echo().Validator = &userValidator{validator: v}
	if err := c.Bind(&user); err != nil {
		log.Errorf("Unable to bind to user struct")
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "Unable to parse the request payload")
	}
	if err := c.Validate(user); err != nil {
		log.Errorf("Unable to validate the requested body")
		return echo.NewHTTPError(http.StatusBadRequest, "Unable to validate the	request payload")
	}
	user, err := authenticateUser(context.Background(), user, u.Col)
	if err != nil {
		log.Errorf("Unable to authenticate to database")
		return err
	}
	token, err := CreateToken(user.Email)
	if err != nil {
		log.Errorf("Unable to generate the token")
		return err
	}
	c.Response().Header().Set("x-auth-token", "Bearer "+token)
	return c.JSON(http.StatusOK, User{Email: user.Email})
}

func authenticateUser(ctx context.Context, reqUser User, collection dbiface.CollectionAPI) (User, error) {
	var storedUser User // user in db
	// check whether user exists or not
	res := collection.FindOne(ctx, bson.M{"username": reqUser.Email})
	err := res.Decode(&storedUser)
	if err != nil && err != mongo.ErrNoDocuments {
		log.Errorf("Unable to decode user")
		return storedUser, echo.NewHTTPError(http.StatusUnprocessableEntity, "Unable to decode user")
	}
	if err == mongo.ErrNoDocuments {
		log.Errorf("User %v does not exsists", reqUser.Email)
		return storedUser, echo.NewHTTPError(http.StatusNotFound, "User does not exsists")
	}
	// validate the password
	if !isCredValid(reqUser.Password, storedUser.Password) {
		return storedUser, echo.NewHTTPError(http.StatusUnauthorized, "Credentials invalid")
	}
	return User{Email: storedUser.Email}, nil
}

func isCredValid(givenPwd, storedPwd string) bool {
	if err := bcrypt.CompareHashAndPassword([]byte(storedPwd), []byte(givenPwd)); err != nil {
		return false
	}
	return true
}

var (
	cfg config.Properties
)

func CreateToken(username string) (string, error) {
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		log.Fatalf("Configuration cannot be read: %v", err)
	}
	claims := jwt.MapClaims{}
	claims["authozied"] = true
	claims["username"] = username
	claims["exp"] = time.Now().Add(time.Minute * 15).Unix()
	at := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token, err := at.SignedString([]byte(cfg.JwtTokenSecret))
	if err != nil {
		log.Errorf("Unable to generate the toke %v", err)
		return "", err
	}
	return token, nil
}
