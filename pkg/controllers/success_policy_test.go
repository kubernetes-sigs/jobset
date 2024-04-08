package controllers

import (
	batchv1 "k8s.io/api/batch/v1"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"

	"sigs.k8s.io/jobset/pkg/util/collections"
)

// TestJobMatchesSuccessPolicy tests the jobMatchesSuccessPolicy function.
func TestJobMatchesSuccessPolicy(t *testing.T) {
	js := &jobset.JobSet{
		Spec: jobset.JobSetSpec{
			SuccessPolicy: jobset.SuccessPolicy{
				TargetReplicatedJobs: []string{"job1", "job2"},
			},
		},
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				jobset.ReplicatedJobNameKey: "job1",
			},
		},
	}

	result := jobMatchesSuccessPolicy(js, job)

	expected := true
	if result != expected {
		t.Errorf("Expected %t but got %t", expected, result)
	}
}

// TestReplicatedJobMatchesSuccessPolicy tests the replicatedJobMatchesSuccessPolicy function.
func TestReplicatedJobMatchesSuccessPolicy(t *testing.T) {
	js := &jobset.JobSet{
		Spec: jobset.JobSetSpec{
			SuccessPolicy: jobset.SuccessPolicy{
				TargetReplicatedJobs: []string{"job1", "job2"},
			},
		},
	}
	rjob := &jobset.ReplicatedJob{Name: "job1"}

	result := replicatedJobMatchesSuccessPolicy(js, rjob)

	expected := true
	if result != expected {
		t.Errorf("Expected %t but got %t", expected, result)
	}
}

// TestNumJobsMatchingSuccessPolicy tests the numJobsMatchingSuccessPolicy function.
func TestNumJobsMatchingSuccessPolicy(t *testing.T) {
	js := &jobset.JobSet{
		Spec: jobset.JobSetSpec{
			SuccessPolicy: jobset.SuccessPolicy{
				TargetReplicatedJobs: []string{"job1", "job2"},
			},
		},
	}
	jobs := []*batchv1.Job{
		{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					jobset.ReplicatedJobNameKey: "job1",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					jobset.ReplicatedJobNameKey: "job3",
				},
			},
		},
	}

	result := numJobsMatchingSuccessPolicy(js, jobs)

	expected := 1
	if result != expected {
		t.Errorf("Expected %d but got %d", expected, result)
	}
}

// TestNumJobsExpectedToSucceed tests the numJobsExpectedToSucceed function.
func TestNumJobsExpectedToSucceed(t *testing.T) {
	js := &jobset.JobSet{
		Spec: jobset.JobSetSpec{
			SuccessPolicy: jobset.SuccessPolicy{
				Operator: jobset.OperatorAll,
				TargetReplicatedJobs: []string{"job1", "job2"},
			},
		},
		Spec.ReplicatedJobs = []jobset.ReplicatedJob{
			{Name: "job1", Replicas: 2},
			{Name: "job2", Replicas: 3},
			{Name: "job3", Replicas: 1},
		},
	}

	result := numJobsExpectedToSucceed(js)

	expected := 5
	if result != expected {
		t.Errorf("Expected %d but got %d", expected, result)
	}
}