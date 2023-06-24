import jobset
from jobset.models.jobset_v1alpha2_job_set import JobsetV1alpha2JobSet
from kubernetes.client.models.v1_job_template_spec import V1JobTemplateSpec

jobset_example = JobsetV1alpha2JobSet(
    api_version="0",
    kind="0",
    metadata=None,
    spec=jobset.models.jobset_v1alpha2_job_set_spec.JobsetV1alpha2JobSetSpec(
        failure_policy=jobset.models.jobset_v1alpha2_failure_policy.JobsetV1alpha2FailurePolicy(
            max_restarts=56,
        ),
        replicated_jobs=[
            jobset.models.jobset_v1alpha2_replicated_job.JobsetV1alpha2ReplicatedJob(
                name="0",
                network=jobset.models.jobset_v1alpha2_network.JobsetV1alpha2Network(
                    enable_dns_hostnames=True,
                ),
                replicas=56,
                template=V1JobTemplateSpec(),
            )
        ],
        success_policy=jobset.models.jobset_v1alpha2_success_policy.JobsetV1alpha2SuccessPolicy(
            operator="0",
            target_replicated_jobs=["0"],
        ),
        suspend=True,
    ),
    status=jobset.models.jobset_v1alpha2_job_set_status.JobsetV1alpha2JobSetStatus(
        replicated_jobs_status=[
            jobset.models.jobset_v1alpha2_replicated_job_status.JobsetV1alpha2ReplicatedJobStatus(
                failed=56,
                name="0",
                ready=56,
                succeeded=56,
            )
        ],
        conditions=[None],
        restarts=56,
    ),
)

print(jobset_example)
