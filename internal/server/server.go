package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"github.com/example/pns/internal/config"
	"github.com/example/pns/internal/model"
	"github.com/example/pns/internal/repository"
	"github.com/example/pns/internal/service"
	"github.com/example/pns/internal/web"
)

type Server struct {
	cfg       config.Config
	router    *gin.Engine
	users     *repository.UserRepository
	devices   *repository.DeviceRepository
	promos    *repository.PromotionRepository
	auth      *service.AuthService
	notifier  *service.NotificationService
	ctx       context.Context
}

func New(cfg config.Config, ctx context.Context, repos struct {
	Users   *repository.UserRepository
	Devices *repository.DeviceRepository
	Promos  *repository.PromotionRepository
}) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	store := cookie.NewStore([]byte(cfg.SessionSecret))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   60 * 60 * 12,
		HttpOnly: true,
		// Lax keeps the cookie off cross-site POSTs, so another site cannot
		// submit the admin forms (delete a user, activate a promotion) on an
		// admin's behalf.
		SameSite: http.SameSiteLaxMode,
	})
	r.Use(sessions.Sessions("pns_session", store))

	s := &Server{
		cfg:      cfg,
		router:   r,
		users:    repos.Users,
		devices:  repos.Devices,
		promos:   repos.Promos,
		auth:     service.NewAuthService(repos.Users),
		notifier: service.NewNotificationService(cfg.FCMServerKey),
		ctx:      ctx,
	}

	r.SetHTMLTemplate(web.Templates())
	s.registerRoutes()
	return s
}

func (s *Server) Run() error {
	return s.router.Run(":" + s.cfg.AppPort)
}

func (s *Server) registerRoutes() {
	s.router.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	api := s.router.Group("/api/v1")
	api.Use(s.serverKeyGuard())
	api.POST("/devices/register", s.handleRegisterDevice)

	s.router.GET("/login", s.pageLogin)
	s.router.POST("/login", s.handleLogin)
	s.router.POST("/logout", s.handleLogout)

	authGroup := s.router.Group("/")
	authGroup.Use(s.requireAuth())

	authGroup.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/admin/promotions")
	})

	authGroup.GET("/admin/users", s.pageUsers)
	authGroup.GET("/admin/users/new", s.pageUserNew)
	authGroup.POST("/admin/users", s.handleUserCreate)
	authGroup.GET("/admin/users/:id/edit", s.pageUserEdit)
	authGroup.POST("/admin/users/:id/update", s.handleUserUpdate)
	authGroup.POST("/admin/users/:id/delete", s.handleUserDelete)

	authGroup.GET("/admin/promotions", s.pagePromotions)
	authGroup.GET("/admin/promotions/new", s.pagePromotionNew)
	authGroup.POST("/admin/promotions", s.handlePromotionCreate)
	authGroup.GET("/admin/promotions/:id/edit", s.pagePromotionEdit)
	authGroup.POST("/admin/promotions/:id/update", s.handlePromotionUpdate)
	authGroup.POST("/admin/promotions/:id/activate", s.handlePromotionActivate)
	authGroup.POST("/admin/promotions/:id/deactivate", s.handlePromotionDeactivate)
}

func (s *Server) serverKeyGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetHeader("X-Server-Key")
		if key == "" {
			key = c.GetHeader("server_key")
		}
		if subtle.ConstantTimeCompare([]byte(key), []byte(s.cfg.ServerKey)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid server key"})
			return
		}
		c.Next()
	}
}

func (s *Server) requireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		if session.Get("user_id") == nil {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		c.Next()
	}
}

// --- Device API ---

type registerDeviceRequest struct {
	DeviceID string `json:"device_id" binding:"required"`
	Token    string `json:"fcm_device_token" binding:"required"`
}

func (s *Server) handleRegisterDevice(c *gin.Context) {
	var req registerDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	dt, err := s.devices.UpsertActive(c.Request.Context(), req.DeviceID, req.Token)
	if err != nil {
		log.Printf("register device %q: %v", req.DeviceID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not register device"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": dt,
	})
}

// --- Auth pages ---

func (s *Server) pageLogin(c *gin.Context) {
	session := sessions.Default(c)
	if session.Get("user_id") != nil {
		c.Redirect(http.StatusFound, "/")
		return
	}
	c.HTML(http.StatusOK, "login.tmpl", gin.H{"title": "Login"})
}

func (s *Server) handleLogin(c *gin.Context) {
	email := c.PostForm("email")
	password := c.PostForm("password")
	user, err := s.auth.Authenticate(c.Request.Context(), email, password)
	if err != nil {
		c.HTML(http.StatusUnauthorized, "login.tmpl", gin.H{"error": "invalid credentials"})
		return
	}
	session := sessions.Default(c)
	session.Set("user_id", user.ID)
	session.Set("user_email", user.Email)
	_ = session.Save()
	c.Redirect(http.StatusFound, "/")
}

func (s *Server) handleLogout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	_ = session.Save()
	c.Redirect(http.StatusFound, "/login")
}

// --- User management ---

func (s *Server) pageUsers(c *gin.Context) {
	users, err := s.users.List(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.HTML(http.StatusOK, "users.tmpl", gin.H{"users": users})
}

func (s *Server) pageUserNew(c *gin.Context) {
	c.HTML(http.StatusOK, "user_new.tmpl", nil)
}

func (s *Server) handleUserCreate(c *gin.Context) {
	email := c.PostForm("email")
	password := c.PostForm("password")
	if email == "" || password == "" {
		c.HTML(http.StatusBadRequest, "user_new.tmpl", gin.H{"error": "email and password required"})
		return
	}
	hash, err := s.auth.HashPassword(password)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.users.Create(c.Request.Context(), email, hash); err != nil {
		c.HTML(http.StatusBadRequest, "user_new.tmpl", gin.H{"error": err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/admin/users")
}

func (s *Server) pageUserEdit(c *gin.Context) {
	id := c.Param("id")
	user, err := s.users.Get(c.Request.Context(), id)
	if err != nil {
		c.String(http.StatusNotFound, "user not found")
		return
	}
	c.HTML(http.StatusOK, "user_edit.tmpl", gin.H{"user": user})
}

func (s *Server) handleUserUpdate(c *gin.Context) {
	id := c.Param("id")
	email := c.PostForm("email")
	password := c.PostForm("password")
	var hash string
	if password != "" {
		var err error
		hash, err = s.auth.HashPassword(password)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := s.users.Update(c.Request.Context(), id, email, hash); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/users")
}

func (s *Server) handleUserDelete(c *gin.Context) {
	id := c.Param("id")
	if err := s.users.Delete(c.Request.Context(), id); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/users")
}

// --- Promotions ---

func (s *Server) pagePromotions(c *gin.Context) {
	promos, err := s.promos.List(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.HTML(http.StatusOK, "promotions.tmpl", gin.H{"promotions": promos})
}

func (s *Server) pagePromotionNew(c *gin.Context) {
	c.HTML(http.StatusOK, "promotion_new.tmpl", nil)
}

func (s *Server) pagePromotionEdit(c *gin.Context) {
	id := c.Param("id")
	promo, err := s.promos.Get(c.Request.Context(), id)
	if err != nil {
		c.String(http.StatusNotFound, "promotion not found")
		return
	}
	c.HTML(http.StatusOK, "promotion_edit.tmpl", gin.H{"promo": promo})
}

func (s *Server) handlePromotionCreate(c *gin.Context) {
	promo, err := s.promotionFromRequest(c)
	if err != nil {
		c.HTML(http.StatusBadRequest, "promotion_new.tmpl", gin.H{"error": err.Error()})
		return
	}
	promo.Status = "created"
	if _, err := s.promos.Create(c.Request.Context(), promo); err != nil {
		c.HTML(http.StatusBadRequest, "promotion_new.tmpl", gin.H{"error": err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/admin/promotions")
}

func (s *Server) handlePromotionUpdate(c *gin.Context) {
	id := c.Param("id")
	promo, err := s.promotionFromRequest(c)
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	promo.ID = id
	if err := s.promos.Update(c.Request.Context(), promo); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/promotions")
}

func (s *Server) handlePromotionActivate(c *gin.Context) {
	id := c.Param("id")
	if err := s.promos.SetStatus(c.Request.Context(), id, "active"); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	go s.pushPromotion(id)
	c.Redirect(http.StatusFound, "/admin/promotions")
}

func (s *Server) handlePromotionDeactivate(c *gin.Context) {
	id := c.Param("id")
	if err := s.promos.SetStatus(c.Request.Context(), id, "inactive"); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/promotions")
}

var promotionStatuses = map[string]bool{"created": true, "active": true, "inactive": true}

func (s *Server) promotionFromRequest(c *gin.Context) (model.Promotion, error) {
	title := strings.TrimSpace(c.PostForm("title"))
	if title == "" {
		return model.Promotion{}, errors.New("title is required")
	}
	start, err := time.Parse("2006-01-02", c.PostForm("start_date"))
	if err != nil {
		return model.Promotion{}, errors.New("start date must be a date (YYYY-MM-DD)")
	}
	end, err := time.Parse("2006-01-02", c.PostForm("end_date"))
	if err != nil {
		return model.Promotion{}, errors.New("end date must be a date (YYYY-MM-DD)")
	}
	if end.Before(start) {
		return model.Promotion{}, errors.New("end date cannot be before the start date")
	}
	status := c.PostForm("status")
	if status == "" {
		status = "created"
	}
	if !promotionStatuses[status] {
		return model.Promotion{}, errors.New("status must be created, active or inactive")
	}
	return model.Promotion{
		Title:       title,
		ImageURL:    c.PostForm("image_url"),
		Description: c.PostForm("description"),
		StartDate:   start,
		EndDate:     end,
		Status:      status,
	}, nil
}

// pushPromotion sends the promotion to every active device token. Each send
// gets its own timeout: a single deadline shared by the whole loop used to
// expire after the first few tokens and silently skip the rest.
func (s *Server) pushPromotion(promoID string) {
	if !s.notifier.Configured() {
		log.Printf("promotion %s activated, but FCM_SERVER_KEY is not set: no pushes sent", promoID)
		return
	}
	ctx := context.Background()
	lookup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	promo, err := s.promos.Get(lookup, promoID)
	if err != nil {
		log.Printf("push promotion %s: load promotion: %v", promoID, err)
		return
	}
	tokens, err := s.devices.ListActiveTokens(lookup)
	if err != nil {
		log.Printf("push promotion %s: load device tokens: %v", promoID, err)
		return
	}

	sent, failed := 0, 0
	for _, t := range tokens {
		sendCtx, cancelSend := context.WithTimeout(ctx, 15*time.Second)
		err := s.notifier.Send(sendCtx, service.PushMessage{
			Token: t.FCMToken,
			Title: promo.Title,
			Body:  promo.Description,
		})
		cancelSend()
		if err != nil {
			failed++
			log.Printf("push promotion %s to device %s: %v", promoID, t.DeviceID, err)
			continue
		}
		sent++
	}
	log.Printf("push promotion %s: %d sent, %d failed, %d tokens", promoID, sent, failed, len(tokens))
}

