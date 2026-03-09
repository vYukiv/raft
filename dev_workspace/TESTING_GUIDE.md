# 运行与测试说明

## 1. 为什么单独写测试脚本

老师原始脚本默认编译 `yourCode/`。

但当前阶段我们是在隔离目录 `dev_workspace/workcode/` 下验证实现，所以新建了两份脚本：

- `dev_workspace/scripts/rafttest_single_workcode.sh`
- `dev_workspace/scripts/rafttest_workcode.sh`

目的：

- 不改老师原脚本
- 不改老师原始 `yourCode` 目录
- 让隔离副本可以直接复用老师的 `tests/` 与 `raftproxy`

## 2. 当前运行链路

### 2.1 单条测试脚本

文件：

- `dev_workspace/scripts/rafttest_single_workcode.sh`

它的执行流程是：

1. 进入 `dev_workspace/workcode`
2. 使用 Go 编译当前实现，输出到 `dev_workspace/bin/raftrunner`
3. 进入老师的 `tests/`
4. 编译：
   - `raftproxyrunner`
   - `rafttest`
5. 随机生成 5 个真实节点端口、5 个代理端口、1 个测试器端口
6. 启动 5 个 proxy 进程
7. 启动 5 个 Raft 节点进程
8. 调用老师的测试二进制执行指定测试名
9. 测试完成后杀掉所有节点与代理进程

### 2.2 全量测试脚本

文件：

- `dev_workspace/scripts/rafttest_workcode.sh`

它会：

1. 一次性生成一组端口
2. 依次调用单条脚本，顺序运行全部 10 个测试：
   - 6 个 leader election
   - 4 个 log replication

## 3. 当前脚本和老师原脚本的区别

老师原脚本：

- 编译目录是 `yourCode/`
- 输出二进制到 `raft-main/bin/`

当前工作脚本：

- 编译目录改为 `dev_workspace/workcode/`
- 输出二进制到 `dev_workspace/bin/`
- 测试代码仍然复用老师的 `tests/`
- 节点/代理的启动参数与老师原流程保持一致

也就是说：

- 编译目标换了
- 测试框架没换
- 端口组织逻辑没换
- grader 行为没换

## 4. 运行前提

当前机器上已经补好了 Go 环境。

使用时需要保证：

- Go 在 PATH 中可见
- 当前仓库目录仍保留：
  - `tests/`
  - `raft.proto`
  - `dev_workspace/workcode`

## 5. 常用命令

### 5.1 跑单条测试

```bash
export PATH="/opt/homebrew/bin:$PATH"
bash "/Users/wk/Desktop/cmsc5735/lab/raft-main/dev_workspace/scripts/rafttest_single_workcode.sh" testOneCandidateOneRoundElection
```

### 5.2 跑第一条日志复制测试

```bash
export PATH="/opt/homebrew/bin:$PATH"
bash "/Users/wk/Desktop/cmsc5735/lab/raft-main/dev_workspace/scripts/rafttest_single_workcode.sh" testOneSimplePut
```

### 5.3 跑全量测试

```bash
export PATH="/opt/homebrew/bin:$PATH"
bash "/Users/wk/Desktop/cmsc5735/lab/raft-main/dev_workspace/scripts/rafttest_workcode.sh"
```

## 6. 已实际跑过的测试

以下测试已经用当前脚本真实跑过：

### Leader Election

- `testOneCandidateOneRoundElection`
- `testOneCandidateStartTwoElection`
- `testTwoCandidateForElection`
- `testSplitVote`
- `testAllForElection`
- `testLeaderRevertToFollower`

### Log Replication

- `testOneSimplePut`
- `testOneSimpleUpdate`
- `testOneSimpleDelete`
- `testDeleteNonExistKey`

## 7. 当前通过情况

通过：

- `testOneCandidateOneRoundElection`
- `testOneCandidateStartTwoElection`
- `testTwoCandidateForElection`
- `testSplitVote`
- `testAllForElection`
- `testLeaderRevertToFollower`
- `testOneSimplePut`
- `testOneSimpleUpdate`

失败：

- `testOneSimpleDelete`
- `testDeleteNonExistKey`

## 8. 当前失败测试的判读方式

剩余失败用例的日志重点要看三类事件：

- leader 发给 follower 的 `AppendEntries`
- follower 返回的 `matchIdx`
- 不同时间点 `GetValue` 对 key 的可见状态

判断标准不是“日志有没有复制出去”这么简单，而是：

- 某次 append 后是否应该立刻 commit
- 某次 commit 是否应该立刻广播给所有 follower
- delete 操作的本地生效时机是否早于 grader 期望

## 9. 目前的调试建议

如果继续追剩余两个测试，最值得观察的是：

- leader 在每轮 heartbeat 前后的：
  - `commitIndex`
  - `lastApplied`
  - `matchIndex`
  - `nextIndex`
- delete 日志到达多数派后：
  - 是不是太早应用到了 leader 的本地 KV
  - 是不是太早通过 `LeaderCommit` 广播给 follower

## 10. 当前脚本的价值

这两份脚本现在已经可以作为后续调试基座：

- 改完逻辑后可以直接跑单条验证
- 稳定后再跑全量回归
- 不需要碰老师原始脚本
- 方便后续把 `workcode/main.go` 合并回正式提交目录
