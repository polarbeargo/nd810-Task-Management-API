package handlers

import (
	"errors"
	"net/http"

	"task-manager/backend/internal/models"
	"task-manager/backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/gofrs/uuid"
	"gorm.io/gorm"
)

type TaskHandler struct {
	db          *gorm.DB
	taskService services.TaskService
}

func (h *TaskHandler) CreateTask(c *gin.Context) {
	var taskInput struct {
		UserID      string `json:"user_id" binding:"required"`
		Title       string `json:"title" binding:"required"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}
	if err := c.ShouldBindJSON(&taskInput); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	task := models.Task{
		UserID:      uuid.FromStringOrNil(taskInput.UserID),
		Title:       taskInput.Title,
		Description: taskInput.Description,
		Status:      taskInput.Status,
	}
	err := h.taskService.CreateTask(h.db, task)
	if err != nil {
		handleTaskError(c, err)
		return
	}
	c.JSON(http.StatusCreated, task)
}

func NewTaskHandler(db *gorm.DB, taskService services.TaskService) *TaskHandler {
	return &TaskHandler{db: db, taskService: taskService}
}

func (h *TaskHandler) UpdateTask(c *gin.Context) {
	idStr := c.Param("id")
	id := uuid.FromStringOrNil(idStr)
	var taskInput struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}
	if err := c.ShouldBindJSON(&taskInput); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updated := models.Task{
		Title:       taskInput.Title,
		Description: taskInput.Description,
		Status:      taskInput.Status,
	}
	err := h.taskService.UpdateTask(h.db, id, updated)
	if err != nil {
		handleTaskError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "task updated successfully"})
}

func (h *TaskHandler) DeleteTask(c *gin.Context) {
	idStr := c.Param("id")
	id := uuid.FromStringOrNil(idStr)
	err := h.taskService.DeleteTask(h.db, id)
	if err != nil {
		handleTaskError(c, err)
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

func (h *TaskHandler) GetTaskByID(c *gin.Context) {
	idStr := c.Param("id")
	id := uuid.FromStringOrNil(idStr)
	task, err := h.taskService.GetTaskByID(h.db, id)
	if err != nil {
		handleTaskError(c, err)
		return
	}
	c.JSON(http.StatusOK, task)
}

func (h *TaskHandler) GetTasksByUser(c *gin.Context) {
	userIDStr := c.Param("user_id")
	userID := uuid.FromStringOrNil(userIDStr)
	var tasks []models.Task
	result := h.db.Where("user_id = ?", userID).Find(&tasks)
	if result.Error != nil {
		handleTaskError(c, result.Error)
		return
	}
	c.JSON(http.StatusOK, tasks)
}

func (h *TaskHandler) GetTasks(c *gin.Context) {

	sortBy := c.DefaultQuery("sortBy", "created_at")
	order := c.DefaultQuery("order", "desc")
	page := c.DefaultQuery("page", "1")
	pageSize := c.DefaultQuery("pageSize", "10")

	tasks, total, err := h.taskService.GetTasksPaginated(h.db, sortBy, order, page, pageSize)
	if err != nil {
		handleTaskError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"tasks": tasks,
		"total": total,
	})
}

func handleTaskError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "task not found",
		})
	} else {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to process task request",
		})
	}
}
