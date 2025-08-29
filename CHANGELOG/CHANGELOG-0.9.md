## v0.9.0

Changes since `v0.8.0`.

### New Features

- Implement RecreateJob failure policy action ([#920](https://github.com/kubernetes-sigs/jobset/pull/920) by [@carreter](https://github.com/carreter))
- feat: support multiple items in DependsOn API ([#878](https://github.com/kubernetes-sigs/jobset/pull/878) by [@Electronic-Waste](https://github.com/Electronic-Waste))
- Enable TLS metrics ([#863](https://github.com/kubernetes-sigs/jobset/pull/863) by [@atiratree](https://github.com/atiratree))
- Add a JobSet label for Pods with the UID of the JobSet. ([#862](https://github.com/kubernetes-sigs/jobset/pull/862) by [@giuliano-sider](https://github.com/giuliano-sider))
- Add group labels / annotations for replicated jobs grouping ([#822](https://github.com/kubernetes-sigs/jobset/pull/822) by [@GiuseppeTT](https://github.com/GiuseppeTT))

### Bug Fixes

- Revert RecreateJob failure policy action addition ([#982](https://github.com/kubernetes-sigs/jobset/pull/982) by [@carreter](https://github.com/carreter))
- fix: handle nil Completions in coordinator validation ([#931](https://github.com/kubernetes-sigs/jobset/pull/931) by [@Ladicle](https://github.com/Ladicle))
- Fix infinite reconciliation when patching JobSets ([#927](https://github.com/kubernetes-sigs/jobset/pull/927) by [@astefanutti](https://github.com/astefanutti))
- fix: certificate fails to find issuer ([#868](https://github.com/kubernetes-sigs/jobset/pull/868) by [@ChenYi015](https://github.com/ChenYi015))
- bug fix: fix depends on validation if there are no replicated jobs ([#819](https://github.com/kubernetes-sigs/jobset/pull/819) by [@kannon92](https://github.com/kannon92))
- fix wrong cmd of output service ([#815](https://github.com/kubernetes-sigs/jobset/pull/815) by [@Monokaix](https://github.com/Monokaix))
- Fix helm charts having wrong repository name ([#803](https://github.com/kubernetes-sigs/jobset/pull/803) by [@kannon92](https://github.com/kannon92))
- fix(sdk): Add Kubernetes refs to the JobSet OpenAPI swagger ([#810](https://github.com/kubernetes-sigs/jobset/pull/810) by [@andreyvelich](https://github.com/andreyvelich))

### Improvements

- Change IndividualJobRecreates to IndividualJobStatus.Recreates ([#958](https://github.com/kubernetes-sigs/jobset/pull/958) by [@carreter](https://github.com/carreter))
- separate metrics one label [<name>/<namespace>] to two label [<name>, <namespace>] ([#889](https://github.com/kubernetes-sigs/jobset/pull/889) by [@googs1025](https://github.com/googs1025))
- Simplify replicatedJob name validations ([#881](https://github.com/kubernetes-sigs/jobset/pull/881) by [@tenzen-y](https://github.com/tenzen-y))
- chore: Replace completionModePtr with ptr.To() ([#883](https://github.com/kubernetes-sigs/jobset/pull/883) by [@tenzen-y](https://github.com/tenzen-y))
- mention field paths and values of invalid fields ([#869](https://github.com/kubernetes-sigs/jobset/pull/869) by [@atiratree](https://github.com/atiratree))
- remove omitempty annotation for restarts field in JobSet status ([#905](https://github.com/kubernetes-sigs/jobset/pull/905) by [@courageJ](https://github.com/courageJ))
- Default to 8443 as metrics port to align service instead of 8080 ([#844](https://github.com/kubernetes-sigs/jobset/pull/844) by [@ardaguclu](https://github.com/ardaguclu))
- Set readOnlyRootFilesystem explicitly to true ([#845](https://github.com/kubernetes-sigs/jobset/pull/845) by [@ardaguclu](https://github.com/ardaguclu))

### Documentation

- add a roadmap and link out to our project ([#968](https://github.com/kubernetes-sigs/jobset/pull/968) by [@kannon92](https://github.com/kannon92))
- Add dev environment setup guidelines to docs site ([#964](https://github.com/kubernetes-sigs/jobset/pull/964) by [@carreter](https://github.com/carreter))
- Rework "Tasks" docs site section and add detailed failure policy examples ([#951](https://github.com/kubernetes-sigs/jobset/pull/951) by [@carreter](https://github.com/carreter))
- Minor docs site cleanup + API update ([#950](https://github.com/kubernetes-sigs/jobset/pull/950) by [@carreter](https://github.com/carreter))
- Update failure policy KEP with proposed RecreateJob behavior ([#925](https://github.com/kubernetes-sigs/jobset/pull/925) by [@carreter](https://github.com/carreter))
- fix e2e testing list and add a 2025 roadmap ([#916](https://github.com/kubernetes-sigs/jobset/pull/916) by [@kannon92](https://github.com/kannon92))
- Fix dead URLs in docs ([#910](https://github.com/kubernetes-sigs/jobset/pull/910) by [@carreter](https://github.com/carreter))
- feat: add example for multiple DependsOn items. ([#895](https://github.com/kubernetes-sigs/jobset/pull/895) by [@Electronic-Waste](https://github.com/Electronic-Waste))
- fix link for depends on ([#896](https://github.com/kubernetes-sigs/jobset/pull/896) by [@kannon92](https://github.com/kannon92))
- update toc for kep 672 ([#890](https://github.com/kubernetes-sigs/jobset/pull/890) by [@kannon92](https://github.com/kannon92))
- docs: support multiple items in DependsOn API ([#877](https://github.com/kubernetes-sigs/jobset/pull/877) by [@Electronic-Waste](https://github.com/Electronic-Waste))
- Fix typo in JobSetStatus.TeminalState docs. ([#871](https://github.com/kubernetes-sigs/jobset/pull/871) by [@mbobrovskyi](https://github.com/mbobrovskyi))
- docs: redirect / to /docs/overview ([#859](https://github.com/kubernetes-sigs/jobset/pull/859) by [@chewong](https://github.com/chewong))
- feat(docs): Create JobSet adopters page ([#852](https://github.com/kubernetes-sigs/jobset/pull/852) by [@andreyvelich](https://github.com/andreyvelich))
- Add example for replicated job groups ([#839](https://github.com/kubernetes-sigs/jobset/pull/839) by [@GiuseppeTT](https://github.com/GiuseppeTT))
- Fix broken helm link to correct one ([#903](https://github.com/kubernetes-sigs/jobset/pull/903) by [@catblade](https://github.com/catblade))
- Use correct links to examples ([#901](https://github.com/kubernetes-sigs/jobset/pull/901) by [@lchrzaszcz](https://github.com/lchrzaszcz))
- Update the helm repo link ([#812](https://github.com/kubernetes-sigs/jobset/pull/812) by [@ahg-g](https://github.com/ahg-g))
- Add minimal helm installation instructions ([#811](https://github.com/kubernetes-sigs/jobset/pull/811) by [@ahg-g](https://github.com/ahg-g))
- Update documentation with latest release ([#808](https://github.com/kubernetes-sigs/jobset/pull/808) by [@kannon92](https://github.com/kannon92))

### Helm & Deployment

- chart(jobset): add controller.hostNetwork toggle ([#978](https://github.com/kubernetes-sigs/jobset/pull/978) by [@m-mamdouhi](https://github.com/m-mamdouhi))
- chore(manifests): Set SeccompProfile in the default JobSet manifests ([#974](https://github.com/kubernetes-sigs/jobset/pull/974) by [@andreyvelich](https://github.com/andreyvelich))
- update helm CRD to include ReplicatedJob groupName ([#870](https://github.com/kubernetes-sigs/jobset/pull/870) by [@atiratree](https://github.com/atiratree))
- move subcharts to nested level ([#842](https://github.com/kubernetes-sigs/jobset/pull/842) by [@kannon92](https://github.com/kannon92))
- Add .helmignore file to ignore files when packging the Helm chart ([#823](https://github.com/kubernetes-sigs/jobset/pull/823) by [@ChenYi015](https://github.com/ChenYi015))

### Dependencies & Build

- Bump github.com/onsi/gomega from 1.38.0 to 1.38.1 ([#986](https://github.com/kubernetes-sigs/jobset/pull/986) by [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump github.com/onsi/ginkgo/v2 from 2.23.4 to 2.25.1 ([#985](https://github.com/kubernetes-sigs/jobset/pull/985) by [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump github.com/stretchr/testify from 1.10.0 to 1.11.0 ([#984](https://github.com/kubernetes-sigs/jobset/pull/984) by [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump the kubernetes group with 7 updates ([#983](https://github.com/kubernetes-sigs/jobset/pull/983) by [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump github.com/prometheus/client_golang from 1.22.0 to 1.23.0 ([#972](https://github.com/kubernetes-sigs/jobset/pull/972) by [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump github.com/onsi/gomega from 1.37.0 to 1.38.0 ([#957](https://github.com/kubernetes-sigs/jobset/pull/957) by [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump the kubernetes group with 7 updates ([#942](https://github.com/kubernetes-sigs/jobset/pull/942) by [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump golang.org/x/oauth2 from 0.21.0 to 0.27.0 in /site/static/examples/client-go ([#940](https://github.com/kubernetes-sigs/jobset/pull/940) by [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump the kubernetes group with 6 updates ([#924](https://github.com/kubernetes-sigs/jobset/pull/924) by [@dependabot[bot]](https://github.com/apps/dependabot))
- Update controller-runtime to 0.21.0 with opa-cert-controller ([#911](https://github.com/kubernetes-sigs/jobset/pull/911) by [@kannon92](https://github.com/kannon92))
- update kind to 0.29.0 ([#913](https://github.com/kubernetes-sigs/jobset/pull/913) by [@kannon92](https://github.com/kannon92))
- Bump the kubernetes group with 6 updates ([#906](https://github.com/kubernetes-sigs/jobset/pull/906) by [@dependabot[bot]](https://github.com/apps/dependabot))
- update golangci to v2 ([#897](https://github.com/kubernetes-sigs/jobset/pull/897) by [@kannon92](https://github.com/kannon92))
- update kubernetes to 0.33 ([#894](https://github.com/kubernetes-sigs/jobset/pull/894) by [@kannon92](https://github.com/kannon92))
- Update kustomize version to v5.2.1 ([#892](https://github.com/kubernetes-sigs/jobset/pull/892) by [@ardaguclu](https://github.com/ardaguclu))
- Bump golang.org/x/net from 0.37.0 to 0.38.0 ([#887](https://github.com/kubernetes-sigs/jobset/pull/887) by [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump postcss from 8.4.21 to 8.5.3 in /site ([#885](https://github.com/kubernetes-sigs/jobset/pull/885) by [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump golang.org/x/net from 0.36.0 to 0.38.0 in /site/static/examples/client-go ([#888](https://github.com/kubernetes-sigs/jobset/pull/888) by [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump golang.org/x/net from 0.33.0 to 0.36.0 in /site/static/examples/client-go ([#886](https://github.com/kubernetes-sigs/jobset/pull/886) by [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump sigs.k8s.io/structured-merge-diff/v4 from 4.6.0 to 4.7.0 in the kubernetes group ([#879](https://github.com/kubernetes-sigs/jobset/pull/879) by [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump github.com/prometheus/client_golang from 1.21.1 to 1.22.0 ([#880](https://github.com/kubernetes-sigs/jobset/pull/880) by [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump github.com/onsi/gomega from 1.36.3 to 1.37.0 ([#867](https://github.com/kubernetes-sigs/jobset/pull/867) by [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump github.com/onsi/ginkgo/v2 from 2.23.3 to 2.23.4 ([#866](https://github.com/kubernetes-sigs/jobset/pull/866) by [@dependabot[bot]](https://github.com/apps/dependabot))
- bump builder golang image to 1.24 ([#864](https://github.com/kubernetes-sigs/jobset/pull/864) by [@atiratree](https://github.com/atiratree))
- Bump sigs.k8s.io/controller-runtime from 0.20.3 to 0.20.4 in the kubernetes group ([#861](https://github.com/kubernetes-sigs/jobset/pull/861) by [@dependabot[bot]](https://github.com/apps/dependabot))
- bump envtest ([#828](https://github.com/kubernetes-sigs/jobset/pull/828) by [@kannon92](https://github.com/kannon92))
- Bump github.com/onsi/gomega from 1.36.2 to 1.36.3 ([#849](https://github.com/kubernetes-sigs/jobset/pull/849) by [@dependabot[bot]](https://github.com/apps/dependabot))
- update to golang 1.24 ([#841](https://github.com/kubernetes-sigs/jobset/pull/841) by [@kannon92](https://github.com/kannon92))
- update k8s dependencies to 0.32.3 without dependabot ([#840](https://github.com/kubernetes-sigs/jobset/pull/840) by [@kannon92](https://github.com/kannon92))
- update controller tools to v0.17.2 ([#833](https://github.com/kubernetes-sigs/jobset/pull/833) by [@kannon92](https://github.com/kannon92))
- Bump github.com/prometheus/client_golang from 1.21.0 to 1.21.1 ([#831](https://github.com/kubernetes-sigs/jobset/pull/831) by [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump github.com/onsi/ginkgo/v2 from 2.22.2 to 2.23.0 ([#832](https://github.com/kubernetes-sigs/jobset/pull/832) by [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump the kubernetes group with 2 updates ([#830](https://github.com/kubernetes-sigs/jobset/pull/830) by [@dependabot[bot]](https://github.com/apps/dependabot))

### Development & Tooling

- Add FailurePolicyAction unit tests ([#967](https://github.com/kubernetes-sigs/jobset/pull/967) by [@carreter](https://github.com/carreter))
- hack/e2e-test.sh: Use a temporary KUBECONFIG ([#966](https://github.com/kubernetes-sigs/jobset/pull/966) by [@tchap](https://github.com/tchap))
- test/e2e: Remove extra Eventually call ([#961](https://github.com/kubernetes-sigs/jobset/pull/961) by [@tchap](https://github.com/tchap))
- test/e2e: Test foreground cascading deletion ([#960](https://github.com/kubernetes-sigs/jobset/pull/960) by [@tchap](https://github.com/tchap))
- e2e tests: Improve remaining condition checks ([#953](https://github.com/kubernetes-sigs/jobset/pull/953) by [@tchap](https://github.com/tchap))
- Adding GiuseppeTT as a reviewer for jobset ([#954](https://github.com/kubernetes-sigs/jobset/pull/954) by [@GiuseppeTT](https://github.com/GiuseppeTT))
- self nominate andreyvelich for approval rights ([#955](https://github.com/kubernetes-sigs/jobset/pull/955) by [@andreyvelich](https://github.com/andreyvelich))
- e2e tests: Improve a flaky DependsOn test ([#949](https://github.com/kubernetes-sigs/jobset/pull/949) by [@tchap](https://github.com/tchap))
- Add Makefile entries for local site development and bump Hugo/docsy versions ([#948](https://github.com/kubernetes-sigs/jobset/pull/948) by [@carreter](https://github.com/carreter))
- e2e tests: Dump namespace content on failure ([#939](https://github.com/kubernetes-sigs/jobset/pull/939) by [@tchap](https://github.com/tchap))
- cleanup: e2e tests: Don't sleep in trainer jobs ([#938](https://github.com/kubernetes-sigs/jobset/pull/938) by [@tchap](https://github.com/tchap))
- gen-sdk.sh: Make sure it works with Podman ([#936](https://github.com/kubernetes-sigs/jobset/pull/936) by [@tchap](https://github.com/tchap))
- Updated Makefile for s390x support ([#933](https://github.com/kubernetes-sigs/jobset/pull/933) by [@carterpewpew](https://github.com/carterpewpew))
- Remove workaround for code-generator ([#932](https://github.com/kubernetes-sigs/jobset/pull/932) by [@yankay](https://github.com/yankay))
- Allow Docker to cache intermediate dependency installation steps ([#922](https://github.com/kubernetes-sigs/jobset/pull/922) by [@carreter](https://github.com/carreter))
- update makefile and apiref ([#919](https://github.com/kubernetes-sigs/jobset/pull/919) by [@atiratree](https://github.com/atiratree))
- chore: Upgrade the default k8s versions for E2E and integration tests cluster ([#882](https://github.com/kubernetes-sigs/jobset/pull/882) by [@tenzen-y](https://github.com/tenzen-y))
- Update main with v0.8.1 changes ([#856](https://github.com/kubernetes-sigs/jobset/pull/856) by [@kannon92](https://github.com/kannon92))
- add helm verify step and fix some minor items ([#851](https://github.com/kubernetes-sigs/jobset/pull/851) by [@kannon92](https://github.com/kannon92))
- add yq to artifacts makefile ([#850](https://github.com/kubernetes-sigs/jobset/pull/850) by [@kannon92](https://github.com/kannon92))
- pin envtest ([#821](https://github.com/kubernetes-sigs/jobset/pull/821) by [@kannon92](https://github.com/kannon92))
