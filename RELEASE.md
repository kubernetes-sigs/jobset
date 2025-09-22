# Release process

To begin the process, open an [issue](https://github.com/kubernetes-sigs/jobset/issues/new/choose)
using the **New Release** template.

## Release cycle

- JobSet aims for at least one minor release every four months, but may release more frequently if important features or bug fixes are ready.
- The release cadence is not rigid and we allow a release may slip, for example,
  due to waiting for an important feature or bug-fixes. However, we strive it
  does not slip more than two weeks.
- When a release slips it does not impact the target release date for the next
  minor release.

## Patch releases

When working on the next N-th minor version of JobSet we continue to maintain
N-1 release. The release branches corresponding to the next patch
releases are regularly tested by CI. Patch releases are released on as-needed
basis.

We follow the Kubernetes cherry-pick [principles](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-release/cherry-picks.md#what-kind-of-prs-are-good-for-cherry-picks).
We will allow cherry-picks for alpha features.
