package commitqueue

import (
	"testing"

	"github.com/evergreen-ci/evergreen/model/build"
	"github.com/evergreen-ci/evergreen/model/event"
	"github.com/evergreen-ci/evergreen/model/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/evergreen-ci/evergreen/db"
	_ "github.com/evergreen-ci/evergreen/testutil"
	"github.com/stretchr/testify/suite"
)

type CommitQueueSuite struct {
	suite.Suite
	q *CommitQueue
}

var sampleCommitQueueItem = CommitQueueItem{
	Issue: "c123",
	Modules: []Module{
		Module{
			Module: "test_module",
			Issue:  "d234",
		},
	},
}

func TestCommitQueueSuite(t *testing.T) {
	s := new(CommitQueueSuite)
	suite.Run(t, s)
}

func (s *CommitQueueSuite) SetupTest() {
	s.Require().NoError(db.ClearCollections(Collection))

	s.q = &CommitQueue{
		ProjectID: "mci",
	}

	s.NoError(InsertQueue(s.q))
	q, err := FindOneId("mci")
	s.Require().NotNil(q)
	s.Require().NoError(err)
}

func (s *CommitQueueSuite) TestEnqueue() {
	pos, err := s.q.Enqueue(sampleCommitQueueItem)
	s.Require().NoError(err)
	s.Equal(0, pos)
	s.Require().Len(s.q.Queue, 1)
	s.Equal("c123", s.q.Queue[0].Issue)
	s.NotEqual(-1, s.q.FindItem("c123"))

	// Persisted to db
	dbq, err := FindOneId("mci")
	s.NoError(err)
	s.Len(dbq.Queue, 1)
	s.Equal(sampleCommitQueueItem.Issue, dbq.Queue[0].Issue)

	// Ensure EnqueueTime set
	s.False(dbq.Queue[0].EnqueueTime.IsZero())

	s.NotEqual(-1, dbq.FindItem("c123"))
}

func (s *CommitQueueSuite) TestEnqueueAtFront() {
	// if queue is empty, puts as the first item
	pos, err := s.q.EnqueueAtFront(sampleCommitQueueItem)
	s.Require().NoError(err)
	s.Equal(pos, 0)

	dbq, err := FindOneId("mci")
	s.NoError(err)
	s.Len(dbq.Queue, 1)

	// insert different items
	item := sampleCommitQueueItem
	item.Issue = "456"
	_, err = s.q.Enqueue(item)
	s.Require().NoError(err)
	item.Issue = "789"
	pos, err = s.q.Enqueue(item)
	s.Require().NoError(err)
	s.Equal(2, pos)

	item.Issue = "critical"
	pos, err = s.q.EnqueueAtFront(item)
	s.Require().NoError(err)
	s.Equal(1, pos)

	dbq, err = FindOneId("mci")
	s.NoError(err)
	s.Require().Len(dbq.Queue, 4)
	s.Equal("critical", dbq.Queue[1].Issue)
}

func (s *CommitQueueSuite) TestUpdateVersion() {
	_, err := s.q.Enqueue(sampleCommitQueueItem)
	s.NoError(err)

	item := s.q.Queue[0]
	item.Version = "my_version"
	s.NoError(s.q.UpdateVersion(item))

	dbq, err := FindOneId("mci")
	s.NoError(err)
	s.Len(dbq.Queue, 1)

	s.Equal(item.Issue, dbq.Queue[0].Issue)
	s.Equal(item.Version, dbq.Queue[0].Version)
}

func (s *CommitQueueSuite) TestNext() {
	// nothing is enqueued
	next, valid := s.q.Next()
	s.False(valid)
	s.Empty(next.Issue)

	// enqueue something
	pos, err := s.q.Enqueue(sampleCommitQueueItem)
	s.NoError(err)
	s.Equal(0, pos)

	// get it off the queue
	next, valid = s.q.Next()
	s.True(valid)
	s.Equal("c123", next.Issue)
}

func (s *CommitQueueSuite) TestRemoveOne() {
	item := sampleCommitQueueItem
	pos, err := s.q.Enqueue(item)
	s.Require().NoError(err)
	s.Require().Equal(0, pos)
	item.Issue = "d234"
	pos, err = s.q.Enqueue(item)
	s.Require().NoError(err)
	s.Require().Equal(1, pos)
	item.Issue = "e345"
	pos, err = s.q.Enqueue(item)
	s.Require().NoError(err)
	s.Require().Equal(2, pos)
	s.Require().Len(s.q.Queue, 3)

	found, err := s.q.Remove("not_here")
	s.NoError(err)
	s.False(found)

	found, err = s.q.Remove("d234")
	s.NoError(err)
	s.True(found)
	items := s.q.Queue
	s.Len(items, 2)
	// Still in order
	s.Equal("c123", items[0].Issue)
	s.Equal("e345", items[1].Issue)

	// Persisted to db
	dbq, err := FindOneId("mci")
	s.NoError(err)
	items = dbq.Queue
	s.Len(items, 2)
	s.Equal("c123", items[0].Issue)
	s.Equal("e345", items[1].Issue)

	s.NoError(s.q.SetProcessing(true))
	found, err = s.q.Remove("c123")
	s.True(found)
	s.NoError(err)
	s.NotNil(s.q.Queue[0])
	s.Equal(s.q.Queue[0].Issue, "e345")
	s.False(s.q.Processing)
}

// can only update processing successfully if the status will actually be changed
func (s *CommitQueueSuite) TestProcessing() {
	s.NoError(s.q.SetProcessing(true))
	s.Error(s.q.SetProcessing(true))
	s.NoError(s.q.SetProcessing(false))
	s.NoError(s.q.SetProcessing(false))
}

func (s *CommitQueueSuite) TestClearAll() {
	item := sampleCommitQueueItem
	pos, err := s.q.Enqueue(item)
	s.Require().NoError(err)
	s.Require().Equal(0, pos)
	item.Issue = "d234"
	pos, err = s.q.Enqueue(item)
	s.Require().NoError(err)
	s.Require().Equal(1, pos)
	item.Issue = "e345"
	pos, err = s.q.Enqueue(item)
	s.Require().NoError(err)
	s.Require().Equal(2, pos)
	s.Require().Len(s.q.Queue, 3)

	q := &CommitQueue{
		ProjectID: "logkeeper",
		Queue:     []CommitQueueItem{},
	}
	s.Require().NoError(InsertQueue(q))

	// Only one commit queue has contents
	clearedCount, err := ClearAllCommitQueues()
	s.NoError(err)
	s.Equal(1, clearedCount)

	s.q, err = FindOneId("mci")
	s.NoError(err)
	s.Empty(s.q.Queue)
	q, err = FindOneId("logkeeper")
	s.NoError(err)
	s.Empty(q.Queue)

	// both have contents
	item.Issue = "c1234"
	pos, err = s.q.Enqueue(item)
	s.Require().NoError(err)
	s.Require().Equal(0, pos)
	item.Issue = "d234"
	pos, err = q.Enqueue(item)
	s.Require().NoError(err)
	s.Require().Equal(0, pos)
	clearedCount, err = ClearAllCommitQueues()
	s.NoError(err)
	s.Equal(2, clearedCount)
}

func (s *CommitQueueSuite) TestCommentTrigger() {
	comment := "no dice"
	action := "created"
	s.False(TriggersCommitQueue(action, comment))

	comment = triggerComment
	s.True(TriggersCommitQueue(action, comment))

	action = "deleted"
	s.False(TriggersCommitQueue(action, comment))
}

func (s *CommitQueueSuite) TestFindOneId() {
	s.NoError(db.ClearCollections(Collection))
	cq := &CommitQueue{ProjectID: "mci"}
	s.NoError(InsertQueue(cq))

	cq, err := FindOneId("mci")
	s.NoError(err)
	s.Equal("mci", cq.ProjectID)

	cq, err = FindOneId("not_here")
	s.NoError(err)
	s.Nil(cq)
}

func TestPreventMergeForItemPR(t *testing.T) {
	assert.NoError(t, db.ClearCollections(event.SubscriptionsCollection))

	patchID := "abcdef012345"
	patchSub := event.NewExpiringPatchOutcomeSubscription(patchID, event.NewGithubMergeSubscriber(event.GithubMergeSubscriber{}))
	require.NoError(t, patchSub.Upsert())

	item := CommitQueueItem{
		Issue:   "1234",
		Version: patchID,
	}

	assert.NoError(t, preventMergeForItem(PRPatchType, false, item, "user"))
	subscriptions, err := event.FindSubscriptions(event.ResourceTypePatch, []event.Selector{{Type: event.SelectorID, Data: item.Version}})
	assert.NoError(t, err)
	assert.Empty(t, subscriptions)
}

func TestPreventMergeForItemCLI(t *testing.T) {
	assert.NoError(t, db.ClearCollections(event.SubscriptionsCollection, task.Collection, build.Collection))

	patchID := "abcdef012345"
	patchSub := event.NewExpiringPatchOutcomeSubscription(patchID, event.NewCommitQueueDequeueSubscriber())
	require.NoError(t, patchSub.Upsert())

	item := CommitQueueItem{
		Issue: patchID,
	}

	mergeBuild := &build.Build{Id: "b1", Tasks: []build.TaskCache{{Id: "t1", Activated: true}}}
	require.NoError(t, mergeBuild.Insert())
	mergeTask := &task.Task{Id: "t1", CommitQueueMerge: true, Version: patchID, BuildId: "b1"}
	require.NoError(t, mergeTask.Insert())

	// Without a corresponding version
	assert.NoError(t, preventMergeForItem(CLIPatchType, false, item, "user"))
	subscriptions, err := event.FindSubscriptions(event.ResourceTypePatch, []event.Selector{{Type: event.SelectorID, Data: patchID}})
	assert.NoError(t, err)
	assert.NotEmpty(t, subscriptions)

	mergeTask, err = task.FindOneId("t1")
	assert.NoError(t, err)
	assert.Equal(t, int64(0), mergeTask.Priority)

	// With a corresponding version
	assert.NoError(t, preventMergeForItem(CLIPatchType, true, item, "user"))
	subscriptions, err = event.FindSubscriptions(event.ResourceTypePatch, []event.Selector{{Type: event.SelectorID, Data: patchID}})
	assert.NoError(t, err)
	assert.Empty(t, subscriptions)

	mergeTask, err = task.FindOneId("t1")
	assert.NoError(t, err)
	assert.Equal(t, int64(-1), mergeTask.Priority)

	mergeBuild, err = build.FindOneId("b1")
	assert.NoError(t, err)
	assert.False(t, mergeBuild.Tasks[0].Activated)
}

func TestClearVersionPatchSubscriber(t *testing.T) {
	require.NoError(t, db.Clear(event.SubscriptionsCollection))

	patchID := "abcdef012345"
	patchSub := event.NewExpiringPatchOutcomeSubscription(patchID, event.NewCommitQueueDequeueSubscriber())
	assert.NoError(t, patchSub.Upsert())

	assert.NoError(t, clearVersionPatchSubscriber(patchID, event.CommitQueueDequeueSubscriberType))
	subs, err := event.FindSubscriptions(event.ResourceTypePatch, []event.Selector{{Type: event.SelectorID, Data: patchID}})
	assert.NoError(t, err)
	assert.Empty(t, subs)
}
