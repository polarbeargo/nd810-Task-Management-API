package cache

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
)

type TestTask struct {
	ID          uuid.UUID              `json:"id"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Status      string                 `json:"status"`
	CreatedAt   time.Time              `json:"created_at"`
	Tags        []string               `json:"tags"`
	Metadata    map[string]interface{} `json:"metadata"`
}

func TestCopyValue_BasicTypes(t *testing.T) {
	tests := []struct {
		name     string
		src      interface{}
		dest     interface{}
		expected interface{}
		wantErr  bool
	}{
		{
			name:     "string copy",
			src:      "hello world",
			dest:     new(string),
			expected: "hello world",
			wantErr:  false,
		},
		{
			name:     "int copy",
			src:      42,
			dest:     new(int),
			expected: 42,
			wantErr:  false,
		},
		{
			name:     "bool copy",
			src:      true,
			dest:     new(bool),
			expected: true,
			wantErr:  false,
		},
		{
			name:     "float64 copy",
			src:      3.14159,
			dest:     new(float64),
			expected: 3.14159,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := copyValue(tt.src, tt.dest)

			if (err != nil) != tt.wantErr {
				t.Errorf("copyValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				switch d := tt.dest.(type) {
				case *string:
					if *d != tt.expected {
						t.Errorf("copyValue() got = %v, want %v", *d, tt.expected)
					}
				case *int:
					if *d != tt.expected {
						t.Errorf("copyValue() got = %v, want %v", *d, tt.expected)
					}
				case *bool:
					if *d != tt.expected {
						t.Errorf("copyValue() got = %v, want %v", *d, tt.expected)
					}
				case *float64:
					if *d != tt.expected {
						t.Errorf("copyValue() got = %v, want %v", *d, tt.expected)
					}
				}
			}
		})
	}
}

func TestCopyValue_ComplexTypes(t *testing.T) {
	originalTask := TestTask{
		ID:          uuid.Must(uuid.NewV4()),
		Title:       "Test Task",
		Description: "This is a test task",
		Status:      "pending",
		CreatedAt:   time.Now(),
		Tags:        []string{"urgent", "backend"},
		Metadata: map[string]interface{}{
			"priority": "high",
			"estimate": 5,
		},
	}

	var copiedTask TestTask
	err := copyValue(originalTask, &copiedTask)
	if err != nil {
		t.Fatalf("copyValue() failed for struct: %v", err)
	}

	if copiedTask.ID != originalTask.ID {
		t.Errorf("ID not copied correctly: got %v, want %v", copiedTask.ID, originalTask.ID)
	}
	if copiedTask.Title != originalTask.Title {
		t.Errorf("Title not copied correctly: got %v, want %v", copiedTask.Title, originalTask.Title)
	}
	if copiedTask.Description != originalTask.Description {
		t.Errorf("Description not copied correctly: got %v, want %v", copiedTask.Description, originalTask.Description)
	}
	if copiedTask.Status != originalTask.Status {
		t.Errorf("Status not copied correctly: got %v, want %v", copiedTask.Status, originalTask.Status)
	}
	if len(copiedTask.Tags) != len(originalTask.Tags) {
		t.Errorf("Tags length not copied correctly: got %v, want %v", len(copiedTask.Tags), len(originalTask.Tags))
	}
	for i, tag := range copiedTask.Tags {
		if tag != originalTask.Tags[i] {
			t.Errorf("Tag[%d] not copied correctly: got %v, want %v", i, tag, originalTask.Tags[i])
		}
	}
}

func TestCopyValue_Slices(t *testing.T) {
	originalSlice := []string{"apple", "banana", "cherry"}
	var copiedSlice []string

	err := copyValue(originalSlice, &copiedSlice)
	if err != nil {
		t.Fatalf("copyValue() failed for slice: %v", err)
	}

	if len(copiedSlice) != len(originalSlice) {
		t.Errorf("Slice length not copied correctly: got %v, want %v", len(copiedSlice), len(originalSlice))
	}

	for i, item := range copiedSlice {
		if item != originalSlice[i] {
			t.Errorf("Slice[%d] not copied correctly: got %v, want %v", i, item, originalSlice[i])
		}
	}
}

func TestCopyValue_Maps(t *testing.T) {
	originalMap := map[string]interface{}{
		"name": "John Doe",
		"age":  30,
		"tags": []string{"developer", "golang"},
	}
	var copiedMap map[string]interface{}

	err := copyValue(originalMap, &copiedMap)
	if err != nil {
		t.Fatalf("copyValue() failed for map: %v", err)
	}

	if len(copiedMap) != len(originalMap) {
		t.Errorf("Map length not copied correctly: got %v, want %v", len(copiedMap), len(originalMap))
	}

	if copiedMap["name"] != originalMap["name"] {
		t.Errorf("Map name not copied correctly: got %v, want %v", copiedMap["name"], originalMap["name"])
	}

	ageValue := copiedMap["age"]
	switch v := ageValue.(type) {
	case float64:
		if v != 30.0 {
			t.Errorf("Map age not copied correctly: got %v, want %v", v, 30.0)
		}
	case int:
		if v != 30 {
			t.Errorf("Map age not copied correctly: got %v, want %v", v, 30)
		}
	default:
		t.Errorf("Map age has unexpected type: %T", v)
	}

	tagsValue := copiedMap["tags"]
	switch v := tagsValue.(type) {
	case []interface{}:
		if len(v) != 2 || v[0] != "developer" || v[1] != "golang" {
			t.Errorf("Map tags not copied correctly: got %v, want %v", v, []interface{}{"developer", "golang"})
		}
	case []string:
		if len(v) != 2 || v[0] != "developer" || v[1] != "golang" {
			t.Errorf("Map tags not copied correctly: got %v, want %v", v, []string{"developer", "golang"})
		}
	default:
		t.Errorf("Map tags has unexpected type: %T", v)
	}
}

func TestCopyValue_InterfacePointer(t *testing.T) {
	original := "test string"
	var dest interface{}

	err := copyValue(original, &dest)
	if err != nil {
		t.Fatalf("copyValue() failed for interface{}: %v", err)
	}

	if dest != original {
		t.Errorf("Interface value not copied correctly: got %v, want %v", dest, original)
	}
}

func TestCopyValue_ErrorCases(t *testing.T) {
	tests := []struct {
		name    string
		src     interface{}
		dest    interface{}
		wantErr bool
	}{
		{
			name:    "non-pointer destination",
			src:     "test",
			dest:    "not a pointer",
			wantErr: true,
		},
		{
			name:    "nil pointer destination",
			src:     "test",
			dest:    (*string)(nil),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := copyValue(tt.src, tt.dest)
			if (err != nil) != tt.wantErr {
				t.Errorf("copyValue() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCopyValue_DeepCopy(t *testing.T) {
	original := TestTask{
		Tags: []string{"tag1", "tag2"},
		Metadata: map[string]interface{}{
			"key": "value",
		},
	}

	var copied TestTask
	err := copyValue(original, &copied)
	if err != nil {
		t.Fatalf("copyValue() failed: %v", err)
	}

	original.Tags[0] = "modified"
	original.Metadata["key"] = "modified"

	if copied.Tags[0] == "modified" {
		t.Error("Deep copy failed: slice was not deep copied")
	}
	if copied.Metadata["key"] == "modified" {
		t.Error("Deep copy failed: map was not deep copied")
	}
}

func BenchmarkCopyValue_SimpleStruct(b *testing.B) {
	task := TestTask{
		ID:          uuid.Must(uuid.NewV4()),
		Title:       "Benchmark Task",
		Description: "This is a benchmark task",
		Status:      "pending",
		CreatedAt:   time.Now(),
		Tags:        []string{"benchmark"},
		Metadata:    map[string]interface{}{"test": true},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var copied TestTask
		_ = copyValue(task, &copied)
	}
}

func BenchmarkCopyValue_LargeSlice(b *testing.B) {
	largeSlice := make([]string, 1000)
	for i := range largeSlice {
		largeSlice[i] = "item"
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var copied []string
		_ = copyValue(largeSlice, &copied)
	}
}
