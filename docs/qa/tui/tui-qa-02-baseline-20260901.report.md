# TUI QA Report

## Summary

- Run ID: tui-qa-02-baseline-20260901
- Run class: baseline
- Final status: PASS (exit 0)
- Certifying: yes
- Revision: 77450807563cbe80bb28cfdb86c187a4be6ac809..59e88eb8f481fc16db5f905ba955006fa07e065d
- Binary: \<run-binary\>/agent-marketplace
- Build class: release-equivalent
- SHA256: `af64f4a389d461cf6a790b69de7a9ad6c45ac4edc8a1c490de77fc46486891bf`
- Upload bytes: 49380/104857600

## Final decision

- all applicable required flows passed

## Flows

| Flow | Required | Status | Profile | Attempts | Evidence | Reason |
|---|---|---|---|---|---|---|
| bare-non-tty-help | yes | PASS | 80x24 | 1 | 2 |  |
| frame-baseline-80x24 | yes | PASS | 80x24 | 1 | 7 |  |
| frame-constrained-50x12 | yes | PASS | 50x12 | 1 | 7 |  |
| launch-bare-tty | yes | PASS | 80x24 | 1 | 7 |  |
| launch-explicit-tui | yes | PASS | 80x24 | 1 | 7 |  |
| no-tui-env | yes | PASS | 80x24 | 1 | 7 |  |
| no-tui-flag | yes | PASS | 80x24 | 1 | 7 |  |
| org-fallback | yes | PASS | 80x24 | 1 | 2 |  |
| quit-ctrl-c | yes | PASS | 80x24 | 1 | 7 |  |
| quit-q | yes | PASS | 80x24 | 1 | 7 |  |
| quit-sigterm | yes | PASS | 80x24 | 1 | 7 |  |
| tui-without-tty | yes | PASS | 80x24 | 1 | 2 |  |

## Findings

None recorded.

## Evidence hashes

- bare-non-tty-help / flows/bare-non-tty-help/attempt-1/driver-log.txt: `c78607a7b293e93c2497b07772c3276fb011c87152f5fda81b8a7edd0a6fd29d` (1789 bytes)
- bare-non-tty-help / flows/bare-non-tty-help/attempt-1/process-cleanup.json: `51af5624e927d7462b68e77f789aa765a5d3ed69d4ede4d30ff26e56e3c2ef51` (123 bytes)
- frame-baseline-80x24 / flows/frame-baseline-80x24/attempt-1/driver-log.txt: `632093b69359ee2cdc031082b58274b70ce706b0cf50ded771bae6bb467037c2` (38 bytes)
- frame-baseline-80x24 / flows/frame-baseline-80x24/attempt-1/flow-result.json: `6a7d2b0262ac3e4cf750f122bb197a88161218913b5c2da1a94f44c6d5be3fd2` (71 bytes)
- frame-baseline-80x24 / flows/frame-baseline-80x24/attempt-1/frame.snapshot.txt: `7a888d382c2cf372718565976ce9b849697f3e7b32d95fa364064b72a62cd28c` (1956 bytes)
- frame-baseline-80x24 / flows/frame-baseline-80x24/attempt-1/lifecycle/lifecycle-124.json: `479c8e13e84e8dbc1ad71919fb503fe31c260d0566ffbebed9a10b135b736c58` (363 bytes)
- frame-baseline-80x24 / flows/frame-baseline-80x24/attempt-1/process-cleanup.json: `f8ffd8f9c50a5ea7fd066482a4384f0c570c9f741c8f0e1cc7d685dc71d26042` (125 bytes)
- frame-baseline-80x24 / flows/frame-baseline-80x24/attempt-1/trace-summary.txt: `e578f480ec36bbcafc63a892c092b8276385d0448d476094fa9ad23a507ecaa4` (3427 bytes)
- frame-constrained-50x12 / flows/frame-constrained-50x12/attempt-1/driver-log.txt: `eddf40f3840ebdb80c2e06b333a063ad3b7e7ee0a01acdbd886bd3aeffaabe78` (41 bytes)
- frame-constrained-50x12 / flows/frame-constrained-50x12/attempt-1/flow-result.json: `9d312951d77f3214378a50fc1538379aad0182d29a5d6130be952a35965a8be1` (74 bytes)
- frame-constrained-50x12 / flows/frame-constrained-50x12/attempt-1/frame.snapshot.txt: `d06b8a631d33c4621b9f1e9dc5b05588af3d8b286a42bfd4d62e09e4fe5a3d83` (622 bytes)
- frame-constrained-50x12 / flows/frame-constrained-50x12/attempt-1/lifecycle/lifecycle-217.json: `559ff3ade34c898564d0fd0ff3c36eecbd12d4b725e4f9c3c5b664a95f54739e` (363 bytes)
- frame-constrained-50x12 / flows/frame-constrained-50x12/attempt-1/process-cleanup.json: `6c04ab892658edf6c8296dec877c5ca60799639fac7e275d7112d6577b56fd1a` (125 bytes)
- frame-constrained-50x12 / flows/frame-constrained-50x12/attempt-1/trace-summary.txt: `d724ccd681d0986ac70499ff8975e83c0abbbf58d59378d3245eb80d1850e8c6` (1301 bytes)
- launch-bare-tty / flows/launch-bare-tty/attempt-1/driver-log.txt: `f4066781f0e84a0c88b13c4ece7c8b8d3b758703d0a4558fd8d68534b3fa5102` (33 bytes)
- launch-bare-tty / flows/launch-bare-tty/attempt-1/flow-result.json: `582ec9b1880219ae1b9f866736acf91c94d6431bd57b2310d4b8809495889462` (66 bytes)
- launch-bare-tty / flows/launch-bare-tty/attempt-1/frame.snapshot.txt: `7a888d382c2cf372718565976ce9b849697f3e7b32d95fa364064b72a62cd28c` (1956 bytes)
- launch-bare-tty / flows/launch-bare-tty/attempt-1/lifecycle/lifecycle-304.json: `bb8230708ee818b47f97c350577b268c4567109af041d01d8c37badc70863dda` (363 bytes)
- launch-bare-tty / flows/launch-bare-tty/attempt-1/process-cleanup.json: `a7b74b77b94ff0dccb6762c3b8c20ee22fb82bf8d46db890118197cdd99c815d` (125 bytes)
- launch-bare-tty / flows/launch-bare-tty/attempt-1/trace-summary.txt: `e578f480ec36bbcafc63a892c092b8276385d0448d476094fa9ad23a507ecaa4` (3427 bytes)
- launch-explicit-tui / flows/launch-explicit-tui/attempt-1/driver-log.txt: `877b5f41757ea85eae68293a42866fc12acf402c24f8989b9b5998d8ea4125f2` (37 bytes)
- launch-explicit-tui / flows/launch-explicit-tui/attempt-1/flow-result.json: `9def0f529ec6e22d6c32f8056178c00fcf03b9991f2d65ab3d3b7f4a89e8ff0a` (70 bytes)
- launch-explicit-tui / flows/launch-explicit-tui/attempt-1/frame.snapshot.txt: `7a888d382c2cf372718565976ce9b849697f3e7b32d95fa364064b72a62cd28c` (1956 bytes)
- launch-explicit-tui / flows/launch-explicit-tui/attempt-1/lifecycle/lifecycle-394.json: `a754be6024bf5bd6045d65517279bcfb05f6b04173c6bf7952dc507297c18fff` (363 bytes)
- launch-explicit-tui / flows/launch-explicit-tui/attempt-1/process-cleanup.json: `e0cc1d00890eefa27988e948ac0754a1dedd83f51394bbff3e605b245e1f2f59` (125 bytes)
- launch-explicit-tui / flows/launch-explicit-tui/attempt-1/trace-summary.txt: `9e2323f4d147b0bd84324f233a67e1da45a127478c00a747159b0feb077c9968` (2886 bytes)
- no-tui-env / flows/no-tui-env/attempt-1/driver-log.txt: `ecf829c669ff84792c81daaee296dc04a3aa5406a6ba5ebd3c8f34acac61433e` (28 bytes)
- no-tui-env / flows/no-tui-env/attempt-1/flow-result.json: `7333742b0ad8a1ab6b201dc21708cedf38a0ce8a35df68fd31db14ab6823ee31` (61 bytes)
- no-tui-env / flows/no-tui-env/attempt-1/frame.snapshot.txt: `ed17811104a845aa166aeee47164231e4defa82c978e64f582643f949666789a` (1944 bytes)
- no-tui-env / flows/no-tui-env/attempt-1/lifecycle/lifecycle-484.json: `67a7dd0bee1aedd53eacded5b70250ea8d26c018a39b72674b1aba22d5aab589` (363 bytes)
- no-tui-env / flows/no-tui-env/attempt-1/process-cleanup.json: `2b063e43d6e358ec9d0906abc2c5d066c76a3df6376ea84122dda0e8a0963d31` (125 bytes)
- no-tui-env / flows/no-tui-env/attempt-1/trace-summary.txt: `7b3e297dc696f6a2b25ff6991fd2fa13e2289450c16f62e960c5bcb932ed28dd` (1805 bytes)
- no-tui-flag / flows/no-tui-flag/attempt-1/driver-log.txt: `318f187ed22c82a8218446d5a0043b5e6c2c6c4c47b96f668fbdb43cb1e4ef06` (29 bytes)
- no-tui-flag / flows/no-tui-flag/attempt-1/flow-result.json: `de9630bdd99f204ac839b5e3206552ab44d47634f7af249c2c8fc5e9072f1b4a` (62 bytes)
- no-tui-flag / flows/no-tui-flag/attempt-1/frame.snapshot.txt: `681d6e7556863b13c2e133f023ef01da48f717001669bf4655667d3c02d935e7` (1944 bytes)
- no-tui-flag / flows/no-tui-flag/attempt-1/lifecycle/lifecycle-560.json: `f8288af206118574365e8498ed25fbe56a76fbf640166b93556f56df604ccef5` (363 bytes)
- no-tui-flag / flows/no-tui-flag/attempt-1/process-cleanup.json: `ffdf476771ec97f5d3c2b678b7c39d101aedb848a4daab2ea580a23e8987b8d7` (125 bytes)
- no-tui-flag / flows/no-tui-flag/attempt-1/trace-summary.txt: `7b3e297dc696f6a2b25ff6991fd2fa13e2289450c16f62e960c5bcb932ed28dd` (1805 bytes)
- org-fallback / flows/org-fallback/attempt-1/driver-log.txt: `8acd469472b9b48da0c0e1af7e1003d15024c29999f60bb38fe6dda039850138` (1186 bytes)
- org-fallback / flows/org-fallback/attempt-1/process-cleanup.json: `e4b688ac71f4eb91c1dae1611739f96e9679ddb42004e1752732ecfc1318791f` (125 bytes)
- quit-ctrl-c / flows/quit-ctrl-c/attempt-1/driver-log.txt: `1caee398ef6f1bcbf78a37dc7f0986519135482d4c89cd8fb3d8526c73f3039e` (29 bytes)
- quit-ctrl-c / flows/quit-ctrl-c/attempt-1/flow-result.json: `3ca8ec8c898cc23600cfd87e1617a8603f0fb328c8c47a3a1c1df195adaaba56` (62 bytes)
- quit-ctrl-c / flows/quit-ctrl-c/attempt-1/frame.snapshot.txt: `7a888d382c2cf372718565976ce9b849697f3e7b32d95fa364064b72a62cd28c` (1956 bytes)
- quit-ctrl-c / flows/quit-ctrl-c/attempt-1/lifecycle/lifecycle-680.json: `e8a36f47542e32ff10bc931a619c1e7958a5e41caa84d97eaf6189b78ae24b9d` (363 bytes)
- quit-ctrl-c / flows/quit-ctrl-c/attempt-1/process-cleanup.json: `311938f1ec5342bcb4e2ce2c4cfac147eeaca4e968910f6e37f70a96eafb44f7` (125 bytes)
- quit-ctrl-c / flows/quit-ctrl-c/attempt-1/trace-summary.txt: `9e2323f4d147b0bd84324f233a67e1da45a127478c00a747159b0feb077c9968` (2886 bytes)
- quit-q / flows/quit-q/attempt-1/driver-log.txt: `b0446338e6344c668b4bdc529714937bd79d4b45019d67d7121500248d472fb0` (24 bytes)
- quit-q / flows/quit-q/attempt-1/flow-result.json: `e9f49bd6877ebe2c1f54963ccc173d0aeb6e13689ba2abc6448577345cf7cc03` (57 bytes)
- quit-q / flows/quit-q/attempt-1/frame.snapshot.txt: `7a888d382c2cf372718565976ce9b849697f3e7b32d95fa364064b72a62cd28c` (1956 bytes)
- quit-q / flows/quit-q/attempt-1/lifecycle/lifecycle-770.json: `8875cbd5811a4cf0510842531445228a41430f85620dde43877fcaf9ce08c28d` (363 bytes)
- quit-q / flows/quit-q/attempt-1/process-cleanup.json: `2bf1bbd7f11aabdef0c1fbf396b387f36c581c5eb7026af212cc24df47a4fef9` (125 bytes)
- quit-q / flows/quit-q/attempt-1/trace-summary.txt: `e578f480ec36bbcafc63a892c092b8276385d0448d476094fa9ad23a507ecaa4` (3427 bytes)
- quit-sigterm / flows/quit-sigterm/attempt-1/driver-log.txt: `a9443df26332e0f7e5acab35082adf1a0d9677bb089e016bcf0134e3d1456098` (30 bytes)
- quit-sigterm / flows/quit-sigterm/attempt-1/flow-result.json: `31a140b18ca66dfb8efb4ba7d98221597a36a57c730d9938d0ba6363c9e627d6` (63 bytes)
- quit-sigterm / flows/quit-sigterm/attempt-1/frame.snapshot.txt: `7a888d382c2cf372718565976ce9b849697f3e7b32d95fa364064b72a62cd28c` (1956 bytes)
- quit-sigterm / flows/quit-sigterm/attempt-1/lifecycle/lifecycle-860.json: `6ab5938576ac1565e4ba31f5699eaf5a0e8eb6a0fcbd258d839ee7fb19178b23` (363 bytes)
- quit-sigterm / flows/quit-sigterm/attempt-1/process-cleanup.json: `69579ff5c969352342c5463e56fc92857ced44e3d4fb49b1daa7408eb8bdec18` (125 bytes)
- quit-sigterm / flows/quit-sigterm/attempt-1/trace-summary.txt: `e578f480ec36bbcafc63a892c092b8276385d0448d476094fa9ad23a507ecaa4` (3427 bytes)
- tui-without-tty / flows/tui-without-tty/attempt-1/driver-log.txt: `a2f9159850d8e4d5fe1b5c40841e8d3f923a4af67c0e91a7bdba9f9394be93d5` (128 bytes)
- tui-without-tty / flows/tui-without-tty/attempt-1/process-cleanup.json: `030d4d237231e5a4f620cc5041c593a806d28b32ce446c94f26966f6d9f2a5a0` (125 bytes)
- frame-baseline-80x24 / raw/frame-baseline-80x24/attempt-1/traces/tests-invariant.spec.ts-frame-baseline-80x24--the-first-nonblank-baseline-frame-fits-80x24-with-trusted-chrome: `d85a190202253de7293a16dda8ee2d4db98c5a079ac807dabce46e39eccce464` (821 bytes)
- frame-constrained-50x12 / raw/frame-constrained-50x12/attempt-1/traces/tests-invariant.spec.ts-frame-constrained-50x12--the-approved-minimum-frame-fits-50x12-with-required-keys-visible: `588832676bf82ac2e5cae27f38ec9fa51e797714b4a16a82d705f05506625b21` (767 bytes)
- launch-bare-tty / raw/launch-bare-tty/attempt-1/traces/tests-invariant.spec.ts-launch-bare-tty--bare-invocation-on-a-TTY-launches-the-TUI: `894cf0ceb8c55d144363e39935f84ef10440befe32004f6d7fff83bf470cb900` (805 bytes)
- launch-explicit-tui / raw/launch-explicit-tui/attempt-1/traces/tests-invariant.spec.ts-launch-explicit-tui--explicit-tui-invocation-on-a-TTY-launches-the-TUI: `f270b4c3dc4b6f56a43720131edc9e886efb3fe0386026c8c97ddd8445c1cfe6` (755 bytes)
- no-tui-env / raw/no-tui-env/attempt-1/traces/tests-invariant.spec.ts-no-tui-env--AGENT\_MARKETPLACE\_NO\_TUI-suppresses-bare-TTY-launch: `6b4b98f4cbafae956fe5dcc30278976e2d508054cc096e60c4d56db44852337b` (1108 bytes)
- no-tui-flag / raw/no-tui-flag/attempt-1/traces/tests-invariant.spec.ts-no-tui-flag----no-tui-suppresses-bare-TTY-launch: `6eb29163749d3d4f60880eea3b08847ba3f94ce26959330896f9239dfd3daf94` (1095 bytes)
- quit-ctrl-c / raw/quit-ctrl-c/attempt-1/traces/tests-invariant.spec.ts-quit-ctrl-c--Ctrl+C-exits-and-restores-the-terminal: `c144a26fa464484987b9ebd985e2760b1b9eb981f328b131437b5b54a9222a7f` (748 bytes)
- quit-q / raw/quit-q/attempt-1/traces/tests-invariant.spec.ts-quit-q--q-exits-and-restores-the-terminal: `da9fbacc5f0c242d553f3ee7c5573202ada6f3228a8060fe3b83dfba786d42b1` (793 bytes)
- quit-sigterm / raw/quit-sigterm/attempt-1/traces/tests-invariant.spec.ts-quit-sigterm--SIGTERM-exits-and-restores-the-terminal: `948104a947f90c16943267f6555d20f869976040af9e1bae67a9496c7c5cf5a5` (805 bytes)

## Environment (allowlisted)

```text
{
  "AM_TUI_QA_RETRIES": "1",
  "AM_TUI_QA_RUN_CLASS": "baseline",
  "AM_TUI_QA_RUN_ID": "tui-qa-02-baseline-20260901"
}
```
