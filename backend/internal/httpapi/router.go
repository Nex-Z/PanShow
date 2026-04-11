package httpapi

import (
	"errors"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"panshow/backend/internal/config"
	"panshow/backend/internal/model"
	"panshow/backend/internal/service"
	"panshow/backend/internal/session"
	"panshow/backend/internal/storage"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const userContextKey = "panshow_user"
const tokenContextKey = "panshow_session_token"
const accessTokenHeader = "X-PanShow-Access-Token"

type RouterDeps struct {
	Config  config.Config
	DB      *gorm.DB
	Session *session.Store
	Storage *storage.Client
}

type API struct {
	cfg     config.Config
	db      *gorm.DB
	session *session.Store
	storage *storage.Client
	r2Cache *responseCache
}

type filesResponse struct {
	Path    string              `json:"path"`
	Entries []storage.FileEntry `json:"entries"`
}

type fileDetailResponse struct {
	File storage.FileEntry `json:"file"`
}

type readmeResponse struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func NewRouter(deps RouterDeps) *gin.Engine {
	api := &API{
		cfg:     deps.Config,
		db:      deps.DB,
		session: deps.Session,
		storage: deps.Storage,
		r2Cache: newResponseCache(),
	}

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	router.Use(cors.New(cors.Config{
		AllowOrigins:     deps.Config.CORSOrigins,
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{"Authorization", "Content-Type", accessTokenHeader},
		ExposeHeaders:    []string{accessTokenHeader},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))
	router.Use(api.cookieSettings())

	router.GET("/healthz", api.health)

	apiRoutes := router.Group("/api")
	apiRoutes.POST("/auth/login", api.login)
	apiRoutes.POST("/auth/logout", api.authRequired(), api.logout)
	apiRoutes.GET("/auth/me", api.authRequired(), api.me)

	public := apiRoutes.Group("", api.accessSession())
	public.POST("/access/password", api.submitDirectoryPassword)
	public.GET("/files", api.listFiles)
	public.GET("/files/detail", api.fileDetail)
	public.GET("/readme", api.readme)
	public.GET("/files/download", api.download)
	public.GET("/files/preview", api.preview)
	public.POST("/files/cache/refresh", api.refreshFileCache)

	admin := apiRoutes.Group("/admin", api.authRequired(), api.adminRequired())
	admin.GET("/status", api.adminStatus)
	admin.GET("/config", api.adminConfig)
	admin.GET("/users", api.listUsers)
	admin.POST("/users", api.createUser)
	admin.PATCH("/users/:id", api.updateUser)
	admin.GET("/directory-passwords", api.listDirectoryPasswords)
	admin.POST("/directory-passwords", api.createDirectoryPassword)
	admin.PATCH("/directory-passwords/:id", api.updateDirectoryPassword)
	admin.DELETE("/directory-passwords/:id", api.disableDirectoryPassword)

	return router
}

func (api *API) health(c *gin.Context) {
	writeJSON(c, http.StatusOK, gin.H{"ok": true})
}

func (api *API) login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if !bindJSON(c, &req) {
		return
	}

	var user model.User
	if err := api.db.Where("username = ?", req.Username).First(&user).Error; err != nil {
		writeError(c, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
		return
	}
	if !user.Active || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		writeError(c, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
		return
	}

	token, err := api.session.Create(c.Request.Context(), user.ID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "session_error", "创建会话失败")
		return
	}
	api.setCookie(c, session.CookieName(), token, int(api.session.TTL().Seconds()))
	writeJSON(c, http.StatusOK, gin.H{"token": token, "user": user})
}

func (api *API) logout(c *gin.Context) {
	token := c.GetString(tokenContextKey)
	_ = api.session.Delete(c.Request.Context(), token)
	api.setCookie(c, session.CookieName(), "", -1)
	writeJSON(c, http.StatusOK, gin.H{"ok": true})
}

func (api *API) me(c *gin.Context) {
	writeJSON(c, http.StatusOK, gin.H{"user": currentUser(c)})
}

func (api *API) listFiles(c *gin.Context) {
	dir, ok := api.normalizedQueryPath(c, "path", "/")
	if !ok {
		return
	}
	if !api.ensureDirectoryAccess(c, dir) {
		return
	}
	cacheKey := listCacheKey(dir)
	var cached filesResponse
	if api.cachedJSON(c, cacheKey, &cached) {
		writeJSON(c, http.StatusOK, cached)
		return
	}
	entries, err := api.storage.List(c.Request.Context(), dir)
	if err != nil {
		writeError(c, http.StatusBadGateway, "storage_error", "读取 R2 文件列表失败")
		return
	}
	response := filesResponse{Path: dir, Entries: entries}
	api.storeCachedJSON(c, cacheKey, response)
	writeJSON(c, http.StatusOK, response)
}

func (api *API) fileDetail(c *gin.Context) {
	filePath, ok := api.normalizedQueryPath(c, "path", "")
	if !ok {
		return
	}
	if filePath == "/" {
		writeError(c, http.StatusBadRequest, "invalid_path", "请选择文件")
		return
	}
	if !api.ensureDirectoryAccess(c, service.ParentDir(filePath)) {
		return
	}
	cacheKey := statCacheKey(filePath)
	var cached fileDetailResponse
	if api.cachedJSON(c, cacheKey, &cached) {
		writeJSON(c, http.StatusOK, cached)
		return
	}
	entry, err := api.storage.Stat(c.Request.Context(), filePath)
	if err != nil {
		writeError(c, http.StatusBadGateway, "storage_error", "读取文件详情失败")
		return
	}
	response := fileDetailResponse{File: entry}
	api.storeCachedJSON(c, cacheKey, response)
	writeJSON(c, http.StatusOK, response)
}

func (api *API) readme(c *gin.Context) {
	dir, ok := api.normalizedQueryPath(c, "path", "/")
	if !ok {
		return
	}
	if !api.ensureDirectoryAccess(c, dir) {
		return
	}
	readmePath := path.Join(dir, "README.md")
	if dir == "/" {
		readmePath = "/README.md"
	}
	cacheKey := readmeCacheKey(dir)
	var cached readmeResponse
	if api.cachedJSON(c, cacheKey, &cached) {
		writeJSON(c, http.StatusOK, cached)
		return
	}
	response := readmeResponse{Path: dir}
	content, err := api.storage.ReadText(c.Request.Context(), readmePath)
	if err != nil {
		api.storeCachedJSON(c, cacheKey, response)
		writeJSON(c, http.StatusOK, response)
		return
	}
	response.Content = content
	api.storeCachedJSON(c, cacheKey, response)
	writeJSON(c, http.StatusOK, response)
}

func (api *API) download(c *gin.Context) {
	filePath, ok := api.normalizedQueryPath(c, "path", "")
	if !ok {
		return
	}
	if filePath == "/" {
		writeError(c, http.StatusBadRequest, "invalid_path", "请选择文件")
		return
	}
	if !api.ensureDirectoryAccess(c, service.ParentDir(filePath)) {
		return
	}
	url, err := api.storage.PresignDownload(c.Request.Context(), filePath, 5*time.Minute)
	if err != nil {
		writeError(c, http.StatusBadGateway, "storage_error", "生成下载链接失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"url": url, "expiresIn": 300})
}

func (api *API) preview(c *gin.Context) {
	filePath, ok := api.normalizedQueryPath(c, "path", "")
	if !ok {
		return
	}
	if filePath == "/" {
		writeError(c, http.StatusBadRequest, "invalid_path", "请选择内容")
		return
	}
	if !api.ensureDirectoryAccess(c, service.ParentDir(filePath)) {
		return
	}
	url, err := api.storage.PresignPreview(c.Request.Context(), filePath, 5*time.Minute)
	if err != nil {
		writeError(c, http.StatusBadGateway, "storage_error", "生成预览链接失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"url": url, "expiresIn": 300})
}

func (api *API) refreshFileCache(c *gin.Context) {
	var req struct {
		Path string `json:"path" binding:"required"`
	}
	if !bindJSON(c, &req) {
		return
	}
	normalized, err := service.NormalizePath(req.Path)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_path", "路径不合法")
		return
	}
	if err := api.session.DeleteCachePatterns(c.Request.Context(), cacheDeletePatterns(normalized)...); err != nil {
		writeError(c, http.StatusInternalServerError, "cache_error", "刷新缓存失败")
		return
	}
	api.r2Cache.DeletePatterns(cacheDeletePatterns(normalized)...)
	writeJSON(c, http.StatusOK, gin.H{"ok": true})
}

func (api *API) submitDirectoryPassword(c *gin.Context) {
	var req struct {
		Path     string `json:"path" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if !bindJSON(c, &req) {
		return
	}
	dir, err := service.NormalizePath(req.Path)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_path", "路径不合法")
		return
	}

	var rule model.DirectoryPassword
	if err := api.db.Where("path = ? AND enabled = ?", dir, true).First(&rule).Error; err != nil {
		writeError(c, http.StatusNotFound, "password_not_configured", "该目录未配置密码")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(rule.PasswordHash), []byte(req.Password)) != nil {
		writeError(c, http.StatusUnauthorized, "invalid_directory_password", "目录密码错误")
		return
	}
	if err := api.session.MarkPasswordPassed(c.Request.Context(), c.GetString(tokenContextKey), rule.Path, rule.Version); err != nil {
		writeError(c, http.StatusInternalServerError, "session_error", "保存目录密码状态失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"ok": true, "path": rule.Path})
}

func (api *API) adminStatus(c *gin.Context) {
	dbOK := api.db.Exec("SELECT 1").Error == nil
	redisOK := api.session.Ping(c.Request.Context()) == nil
	r2OK := api.storage.Health(c.Request.Context()) == nil
	writeJSON(c, http.StatusOK, gin.H{
		"database": dbOK,
		"redis":    redisOK,
		"r2":       r2OK,
	})
}

func (api *API) adminConfig(c *gin.Context) {
	writeJSON(c, http.StatusOK, gin.H{
		"r2Bucket":     api.cfg.R2Bucket,
		"r2RootPrefix": api.cfg.R2RootPrefix,
		"corsOrigins":  api.cfg.CORSOrigins,
	})
}

func (api *API) listUsers(c *gin.Context) {
	var users []model.User
	if err := api.db.Order("id asc").Find(&users).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "database_error", "读取用户失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"users": users})
}

func (api *API) createUser(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
		Role     string `json:"role"`
	}
	if !bindJSON(c, &req) {
		return
	}
	role := req.Role
	if role == "" {
		role = model.RoleUser
	}
	if role != model.RoleAdmin && role != model.RoleUser {
		writeError(c, http.StatusBadRequest, "invalid_role", "角色不合法")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "hash_error", "生成密码哈希失败")
		return
	}
	user := model.User{Username: req.Username, PasswordHash: string(hash), Role: role, Active: true}
	if err := api.db.Create(&user).Error; err != nil {
		writeError(c, http.StatusBadRequest, "database_error", "创建用户失败")
		return
	}
	writeJSON(c, http.StatusCreated, gin.H{"user": user})
}

func (api *API) updateUser(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req struct {
		Password *string `json:"password"`
		Role     *string `json:"role"`
		Active   *bool   `json:"active"`
	}
	if !bindJSON(c, &req) {
		return
	}
	updates := map[string]any{}
	if req.Role != nil {
		if *req.Role != model.RoleAdmin && *req.Role != model.RoleUser {
			writeError(c, http.StatusBadRequest, "invalid_role", "角色不合法")
			return
		}
		updates["role"] = *req.Role
	}
	if req.Active != nil {
		updates["active"] = *req.Active
	}
	if req.Password != nil && *req.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			writeError(c, http.StatusInternalServerError, "hash_error", "生成密码哈希失败")
			return
		}
		updates["password_hash"] = string(hash)
	}
	if len(updates) == 0 {
		writeError(c, http.StatusBadRequest, "empty_update", "没有可更新字段")
		return
	}
	var user model.User
	if err := api.db.First(&user, id).Error; err != nil {
		writeError(c, http.StatusNotFound, "not_found", "用户不存在")
		return
	}
	if err := api.db.Model(&user).Updates(updates).Error; err != nil {
		writeError(c, http.StatusBadRequest, "database_error", "更新用户失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"user": user})
}

func (api *API) listDirectoryPasswords(c *gin.Context) {
	var rules []model.DirectoryPassword
	if err := api.db.Order("path asc").Find(&rules).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "database_error", "读取目录密码失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"directoryPasswords": rules})
}

func (api *API) createDirectoryPassword(c *gin.Context) {
	var req struct {
		Path     string `json:"path" binding:"required"`
		Password string `json:"password" binding:"required"`
		Enabled  *bool  `json:"enabled"`
	}
	if !bindJSON(c, &req) {
		return
	}
	dir, err := service.NormalizePath(req.Path)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_path", "路径不合法")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "hash_error", "生成密码哈希失败")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	rule := model.DirectoryPassword{Path: dir, PasswordHash: string(hash), Enabled: enabled, Version: 1}
	if err := api.db.Create(&rule).Error; err != nil {
		writeError(c, http.StatusBadRequest, "database_error", "创建目录密码失败")
		return
	}
	writeJSON(c, http.StatusCreated, gin.H{"directoryPassword": rule})
}

func (api *API) updateDirectoryPassword(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req struct {
		Path     *string `json:"path"`
		Password *string `json:"password"`
		Enabled  *bool   `json:"enabled"`
	}
	if !bindJSON(c, &req) {
		return
	}
	var rule model.DirectoryPassword
	if err := api.db.First(&rule, id).Error; err != nil {
		writeError(c, http.StatusNotFound, "not_found", "目录密码不存在")
		return
	}
	updates := map[string]any{"version": rule.Version + 1}
	if req.Path != nil {
		dir, err := service.NormalizePath(*req.Path)
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_path", "路径不合法")
			return
		}
		updates["path"] = dir
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.Password != nil && *req.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			writeError(c, http.StatusInternalServerError, "hash_error", "生成密码哈希失败")
			return
		}
		updates["password_hash"] = string(hash)
	}
	if err := api.db.Model(&rule).Updates(updates).Error; err != nil {
		writeError(c, http.StatusBadRequest, "database_error", "更新目录密码失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"directoryPassword": rule})
}

func (api *API) disableDirectoryPassword(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := api.db.Delete(&model.DirectoryPassword{}, id).Error; err != nil {
		writeError(c, http.StatusBadRequest, "database_error", "删除目录密码失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"ok": true})
}

func (api *API) authRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := authToken(c)
		if token == "" {
			writeError(c, http.StatusUnauthorized, "unauthenticated", "请先登录")
			c.Abort()
			return
		}
		userID, err := api.session.UserID(c.Request.Context(), token)
		if err != nil {
			writeError(c, http.StatusUnauthorized, "unauthenticated", "登录已过期")
			c.Abort()
			return
		}
		var user model.User
		if err := api.db.First(&user, userID).Error; err != nil || !user.Active {
			writeError(c, http.StatusUnauthorized, "unauthenticated", "登录已过期")
			c.Abort()
			return
		}
		c.Set(userContextKey, user)
		c.Set(tokenContextKey, token)
		c.Next()
	}
}

func (api *API) accessSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimSpace(c.GetHeader(accessTokenHeader))
		if token == "" {
			cookieToken, err := c.Cookie(session.AccessCookieName())
			if err == nil {
				token = cookieToken
			}
		}
		if len(token) > 128 {
			token = ""
		}
		if token == "" {
			var err error
			token, err = api.session.CreateAccessToken(c.Request.Context())
			if err != nil {
				writeError(c, http.StatusInternalServerError, "session_error", "创建访问会话失败")
				c.Abort()
				return
			}
		}
		api.setCookie(c, session.AccessCookieName(), token, int(api.session.TTL().Seconds()))
		c.Header(accessTokenHeader, token)
		c.Set(tokenContextKey, token)
		api.setOptionalUser(c)
		c.Next()
	}
}

func (api *API) setOptionalUser(c *gin.Context) {
	token := authToken(c)
	if token == "" {
		return
	}
	userID, err := api.session.UserID(c.Request.Context(), token)
	if err != nil {
		return
	}
	var user model.User
	if err := api.db.First(&user, userID).Error; err != nil || !user.Active {
		return
	}
	c.Set(userContextKey, user)
}

func (api *API) adminRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		if currentUser(c).Role != model.RoleAdmin {
			writeError(c, http.StatusForbidden, "forbidden", "需要管理员权限")
			c.Abort()
			return
		}
		c.Next()
	}
}

func (api *API) ensureDirectoryAccess(c *gin.Context, dir string) bool {
	if currentUser(c).Role == model.RoleAdmin {
		return true
	}
	paths := service.DirectoryAncestors(dir)
	var rules []model.DirectoryPassword
	if err := api.db.Where("enabled = ? AND path IN ?", true, paths).Find(&rules).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "database_error", "读取目录密码失败")
		return false
	}
	rulesByPath := make(map[string]model.DirectoryPassword, len(rules))
	for _, rule := range rules {
		rulesByPath[rule.Path] = rule
	}
	token := c.GetString(tokenContextKey)
	for _, protectedPath := range paths {
		rule, ok := rulesByPath[protectedPath]
		if !ok {
			continue
		}
		passed, err := api.session.HasPasswordPassed(c.Request.Context(), token, rule.Path, rule.Version)
		if err != nil {
			writeError(c, http.StatusInternalServerError, "session_error", "读取目录密码状态失败")
			return false
		}
		if !passed {
			writeJSON(c, http.StatusForbidden, gin.H{
				"error": gin.H{
					"code":          "directory_password_required",
					"message":       "需要目录密码",
					"requiredPaths": []string{rule.Path},
				},
			})
			return false
		}
	}
	return true
}

func (api *API) normalizedQueryPath(c *gin.Context, key, fallback string) (string, bool) {
	value := c.Query(key)
	if value == "" {
		value = fallback
	}
	normalized, err := service.NormalizePath(value)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_path", "路径不合法")
		return "", false
	}
	return normalized, true
}

func currentUser(c *gin.Context) model.User {
	user, _ := c.Get(userContextKey)
	typed, _ := user.(model.User)
	return typed
}

func bindJSON(c *gin.Context, req any) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "请求参数不合法")
		return false
	}
	return true
}

func parseID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		writeError(c, http.StatusBadRequest, "invalid_id", "ID 不合法")
		return 0, false
	}
	return uint(id), true
}

func writeJSON(c *gin.Context, status int, payload any) {
	c.JSON(status, payload)
}

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

func authToken(c *gin.Context) string {
	header := c.GetHeader("Authorization")
	if token, ok := strings.CutPrefix(header, "Bearer "); ok {
		return strings.TrimSpace(token)
	}
	token, err := c.Cookie(session.CookieName())
	if err != nil {
		return ""
	}
	return token
}

func (api *API) cookieSettings() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch strings.ToLower(api.cfg.CookieSameSite) {
		case "strict":
			c.SetSameSite(http.SameSiteStrictMode)
		case "none":
			c.SetSameSite(http.SameSiteNoneMode)
		default:
			c.SetSameSite(http.SameSiteLaxMode)
		}
		c.Next()
	}
}

func (api *API) setCookie(c *gin.Context, name, value string, maxAge int) {
	c.SetCookie(name, value, maxAge, "/", "", api.cfg.CookieSecure, true)
}

func (api *API) cachedJSON(c *gin.Context, key string, dest any) bool {
	if api.r2Cache.GetJSON(key, dest) {
		c.Header("X-PanShow-Cache", "local")
		return true
	}
	if ok, err := api.session.GetJSON(c.Request.Context(), key, dest); err == nil && ok {
		api.r2Cache.SetJSON(key, dest, api.cfg.R2CacheTTL)
		c.Header("X-PanShow-Cache", "redis")
		return true
	}
	return false
}

func (api *API) storeCachedJSON(c *gin.Context, key string, value any) {
	api.r2Cache.SetJSON(key, value, api.cfg.R2CacheTTL)
	_ = api.session.SetJSON(c.Request.Context(), key, value, api.cfg.R2CacheTTL)
	c.Header("X-PanShow-Cache", "miss")
}

func isNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

func listCacheKey(dir string) string {
	return "r2:list:" + cacheDirPath(dir)
}

func readmeCacheKey(dir string) string {
	return "r2:readme:" + cacheDirPath(dir)
}

func statCacheKey(filePath string) string {
	return "r2:stat:" + filePath
}

func cacheDirPath(dir string) string {
	if dir == "/" {
		return "/"
	}
	return strings.TrimRight(dir, "/") + "/"
}

func cacheDeletePatterns(targetPath string) []string {
	if targetPath == "/" {
		return []string{"r2:list:*", "r2:readme:*", "r2:stat:*"}
	}
	dir := cacheDirPath(targetPath)
	return []string{
		escapeCachePattern(listCacheKey(targetPath)),
		escapeCachePattern(readmeCacheKey(targetPath)),
		escapeCachePattern(statCacheKey(targetPath)),
		escapeCachePattern("r2:list:"+dir) + "*",
		escapeCachePattern("r2:readme:"+dir) + "*",
		escapeCachePattern("r2:stat:"+dir) + "*",
	}
}

func escapeCachePattern(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"*", "\\*",
		"?", "\\?",
		"[", "\\[",
		"]", "\\]",
	)
	return replacer.Replace(value)
}
