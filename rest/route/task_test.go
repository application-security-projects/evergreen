package route

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/evergreen/db"
	"github.com/evergreen-ci/evergreen/model/artifact"
	"github.com/evergreen-ci/evergreen/model/task"
	"github.com/evergreen-ci/evergreen/model/user"
	"github.com/evergreen-ci/evergreen/rest/data"
	"github.com/evergreen-ci/evergreen/rest/model"
	"github.com/evergreen-ci/gimlet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

////////////////////////////////////////////////////////////////////////
//
// Tests for abort task route

type TaskAbortSuite struct {
	sc   *data.MockConnector
	data data.MockTaskConnector

	suite.Suite
}

func TestTaskAbortSuite(t *testing.T) {
	suite.Run(t, new(TaskAbortSuite))
}

func (s *TaskAbortSuite) SetupSuite() {
	s.data = data.MockTaskConnector{
		CachedTasks: []task.Task{
			{Id: "task1"},
			{Id: "task2"},
		},
		CachedAborted: make(map[string]string),
	}
	s.sc = &data.MockConnector{
		MockTaskConnector: s.data,
	}
}

func (s *TaskAbortSuite) TestAbort() {
	ctx := context.Background()
	ctx = gimlet.AttachUser(ctx, &user.DBUser{Id: "user1"})

	rm := makeTaskAbortHandler(s.sc)
	rm.(*taskAbortHandler).taskId = "task1"
	res := rm.Run(ctx)

	s.Equal(http.StatusOK, res.Status())

	s.NotNil(res)
	s.Equal("user1", s.data.CachedAborted["task1"])
	s.Equal("", s.data.CachedAborted["task2"])
	t, ok := res.Data().(*model.APITask)
	s.True(ok)
	s.Equal(model.ToStringPtr("task1"), t.Id)

	res = rm.Run(ctx)
	s.Equal(http.StatusOK, res.Status())
	s.NotNil(res)
	s.Equal("user1", s.data.CachedAborted["task1"])
	s.Equal("", s.data.CachedAborted["task2"])
	t, ok = (res.Data()).(*model.APITask)
	s.True(ok)
	s.Equal(model.ToStringPtr("task1"), t.Id)
}

func (s *TaskAbortSuite) TestAbortFail() {
	ctx := context.Background()
	ctx = gimlet.AttachUser(ctx, &user.DBUser{Id: "user1"})

	rm := makeTaskAbortHandler(s.sc)
	rm.(*taskAbortHandler).taskId = "task1"
	s.sc.MockTaskConnector.FailOnAbort = true
	resp := rm.Run(ctx)
	s.Equal(http.StatusBadRequest, resp.Status())
}

func TestFetchArtifacts(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	assert.NoError(db.ClearCollections(task.Collection, task.OldCollection, artifact.Collection))
	task1 := task.Task{
		Id:        "task1",
		Execution: 0,
	}
	assert.NoError(task1.Insert())
	assert.NoError(task1.Archive())
	entry := artifact.Entry{
		TaskId:          task1.Id,
		TaskDisplayName: "task",
		BuildId:         "b1",
		Execution:       1,
		Files: []artifact.File{
			{
				Name: "file1",
				Link: "l1",
			},
			{
				Name: "file2",
				Link: "l2",
			},
		},
	}
	assert.NoError(entry.Upsert())
	entry.Execution = 0
	assert.NoError(entry.Upsert())

	task2 := task.Task{
		Id:          "task2",
		Execution:   0,
		DisplayOnly: true,
	}
	assert.NoError(task2.Insert())
	assert.NoError(task2.Archive())

	taskGet := taskGetHandler{taskID: task1.Id, sc: &data.DBConnector{}}
	resp := taskGet.Run(context.Background())
	require.NotNil(resp)
	assert.Equal(resp.Status(), http.StatusOK)
	apiTask := resp.Data().(*model.APITask)
	assert.Len(apiTask.Artifacts, 2)
	assert.Empty(apiTask.PreviousExecutions)

	// fetch all
	taskGet.fetchAllExecutions = true
	resp = taskGet.Run(context.Background())
	require.NotNil(resp)
	assert.Equal(resp.Status(), http.StatusOK)
	apiTask = resp.Data().(*model.APITask)
	require.Len(apiTask.PreviousExecutions, 1)
	assert.NotZero(apiTask.PreviousExecutions[0])
	assert.NotEmpty(apiTask.PreviousExecutions[0].Artifacts)

	// fetchs a display task
	taskGet.taskID = "task2"
	taskGet.fetchAllExecutions = false
	resp = taskGet.Run(context.Background())
	require.NotNil(resp)
	assert.Equal(resp.Status(), http.StatusOK)
	apiTask = resp.Data().(*model.APITask)
	assert.Empty(apiTask.PreviousExecutions)

	// fetch all, tasks with display tasks
	taskGet.fetchAllExecutions = true
	resp = taskGet.Run(context.Background())
	require.NotNil(resp)
	assert.Equal(resp.Status(), http.StatusOK)
	apiTask = resp.Data().(*model.APITask)
	require.Len(apiTask.PreviousExecutions, 1)
	assert.NotZero(apiTask.PreviousExecutions[0])
}

type ProjectTaskWithinDatesSuite struct {
	sc *data.MockConnector
	h  *projectTaskGetHandler

	suite.Suite
}

func TestProjectTaskWithinDatesSuite(t *testing.T) {
	suite.Run(t, new(ProjectTaskWithinDatesSuite))
}

func (s *ProjectTaskWithinDatesSuite) SetupTest() {
	s.h = &projectTaskGetHandler{sc: s.sc}
}

func (s *ProjectTaskWithinDatesSuite) TestParseAllArguments() {
	url := "https://evergreen.mongodb.com/rest/v2/projects/none/versions/tasks" +
		"?status=A" +
		"&status=B" +
		"&started_after=2018-01-01T00%3A00%3A00Z" +
		"&finished_before=2018-02-02T00%3A00%3A00Z"
	r, err := http.NewRequest("GET", url, &bytes.Buffer{})
	s.Require().NoError(err)
	err = s.h.Parse(context.Background(), r)
	s.NoError(err)
	s.Subset([]string{"A", "B"}, s.h.statuses)
	s.Equal(s.h.startedAfter, time.Date(2018, time.January, 1, 0, 0, 0, 0, time.UTC))
	s.Equal(s.h.finishedBefore, time.Date(2018, time.February, 2, 0, 0, 0, 0, time.UTC))
}

func (s *ProjectTaskWithinDatesSuite) TestHasDefaultValues() {
	r, err := http.NewRequest("GET", "https://evergreen.mongodb.com/rest/v2/projects/none/versions/tasks", &bytes.Buffer{})
	s.Require().NoError(err)
	err = s.h.Parse(context.Background(), r)
	s.NoError(err)
	s.Equal([]string(nil), s.h.statuses)
	s.True(s.h.startedAfter.Unix()-time.Now().AddDate(0, 0, -7).Unix() <= 0)
	s.Equal(time.Time{}, s.h.finishedBefore)
}

func TestGetDisplayTaskName(t *testing.T) {
	for testName, testCase := range map[string]func(context.Context, *testing.T){
		"SucceedsWithTaskInDisplayTask": func(ctx context.Context, t *testing.T) {
			tsk := task.Task{Id: "task_id"}
			displayTask := task.Task{
				DisplayName:    "display_task_name",
				ExecutionTasks: []string{tsk.Id},
			}
			require.NoError(t, displayTask.Insert())

			h := makeGetDisplayTaskHandler(&data.MockConnector{
				MockTaskConnector: data.MockTaskConnector{
					CachedTasks: []task.Task{tsk},
				},
			})
			rh, ok := h.(*displayTaskGetHandler)
			require.True(t, ok)
			rh.taskID = tsk.Id

			resp := rh.Run(ctx)
			require.NotNil(t, resp)
			name, ok := resp.Data().(string)
			require.True(t, ok)
			assert.Equal(t, displayTask.DisplayName, name)
		},
		"FailsWithNonexistentTask": func(ctx context.Context, t *testing.T) {
			h := makeGetDisplayTaskHandler(&data.MockConnector{})
			rh, ok := h.(*displayTaskGetHandler)
			require.True(t, ok)
			rh.taskID = "nonexistent"

			resp := rh.Run(ctx)
			require.NotNil(t, resp)
			assert.Equal(t, http.StatusBadRequest, resp.Status())
		},
		"ReturnsOkIfNotPartOfDisplayTask": func(ctx context.Context, t *testing.T) {
			tsk := task.Task{Id: "task_id"}
			h := makeGetDisplayTaskHandler(&data.MockConnector{
				MockTaskConnector: data.MockTaskConnector{
					CachedTasks: []task.Task{tsk},
				},
			})
			rh, ok := h.(*displayTaskGetHandler)
			require.True(t, ok)
			rh.taskID = tsk.Id

			resp := rh.Run(ctx)
			require.NotNil(t, resp)
			assert.Equal(t, http.StatusOK, resp.Status())
		},
	} {
		t.Run(testName, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			require.NoError(t, db.ClearCollections(task.Collection))
			defer func() {
				assert.NoError(t, db.ClearCollections(task.Collection))
			}()

			testCase(ctx, t)
		})
	}

}

func TestGetTaskSyncReadCredentials(t *testing.T) {
	creds := evergreen.S3Credentials{
		Key:    "key",
		Secret: "secret",
		Bucket: "bucket",
	}
	rh := makeTaskSyncReadCredentialsGetHandler(&data.MockConnector{
		MockAdminConnector: data.MockAdminConnector{
			MockSettings: &evergreen.Settings{
				Providers: evergreen.CloudProviders{
					AWS: evergreen.AWSConfig{
						TaskSyncRead: creds,
					},
				},
			},
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp := rh.Run(ctx)
	require.NotNil(t, resp)
	respCreds, ok := resp.Data().(evergreen.S3Credentials)
	require.True(t, ok)
	assert.Equal(t, creds, respCreds)
}

func TestGetTaskSyncPath(t *testing.T) {
	expected := task.Task{
		Id:           "task_id",
		Project:      "project",
		Version:      "version",
		BuildVariant: "build_variant",
		DisplayName:  "name",
	}
	h := makeTaskSyncPathGetHandler(&data.MockConnector{
		MockTaskConnector: data.MockTaskConnector{
			CachedTasks: []task.Task{expected},
		},
	})
	rh, ok := h.(*taskSyncPathGetHandler)
	require.True(t, ok)
	rh.taskID = expected.Id

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp := rh.Run(ctx)

	require.NotNil(t, resp)
	path, ok := resp.Data().(string)
	require.True(t, ok)
	assert.Equal(t, path, expected.S3Path(expected.BuildVariant, expected.DisplayName))
}
