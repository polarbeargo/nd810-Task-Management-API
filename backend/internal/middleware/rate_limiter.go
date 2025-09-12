package middleware

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

func RateLimiter(r rate.Limit, b int) gin.HandlerFunc {
	var visitors = make(map[string]*rate.Limiter)
	var mu sync.Mutex

	getVisitor := func(ip string) *rate.Limiter {
		mu.Lock()
		defer mu.Unlock()
		limiter, exists := visitors[ip]
		if !exists {
			limiter = rate.NewLimiter(r, b)
			visitors[ip] = limiter
		}
		return limiter
	}

	return func(c *gin.Context) {
		ip := c.ClientIP()
		limiter := getVisitor(ip)
		if !limiter.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}
