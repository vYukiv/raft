# Raft 实现状态记录

## 1. 工作目录说明

当前实现没有直接改老师原始骨架目录 `yourCode/`，而是在隔离目录中工作：

- 主实现副本：`dev_workspace/workcode/main.go`
- 单测脚本：`dev_workspace/scripts/rafttest_single_workcode.sh`
- 全量脚本：`dev_workspace/scripts/rafttest_workcode.sh`
- 规划文档：`dev_workspace/PLAN.md`

这样做的目的：

- 不污染老师原始代码框架
- 可以随时回滚或备份当前阶段结果
- 可以先在隔离环境验证逻辑，再决定是否合并回提交目录

## 2. 当前代码结构与实现思路

当前 `workcode/main.go` 仍然保持老师给出的总体框架：

- `main()`
- `NewRaftNode(...)`
- `Propose(...)`
- `GetValue(...)`
- `RequestVote(...)`
- `AppendEntries(...)`
- `SetElectionTimeout(...)`
- `SetHeartBeatInterval(...)`
- `CheckEvents(...)`

没有推翻老师给的接口组织方式，而是在这个框架内部补充状态、channel 和事件循环。

## 3. 当前已补充的核心状态

`raftNode` 中已加入：

- Raft 基本状态：
  - `currentTerm`
  - `votedFor`
  - `log`
  - `commitIndex`
  - `lastApplied`
- Leader 复制状态：
  - `nextIndex`
  - `matchIndex`
- 节点身份状态：
  - `state`
  - `currentLeaderId`
  - `nodeId`
- 运行时配置：
  - `heartBeatInterval`
  - `electionTimeout`
- 内存存储：
  - `kvStore`
- 连接信息：
  - `hostConnectionMap`
- 无锁并发控制 channel：
  - `reqVoteChan`
  - `appendEntryChan`
  - `proposeChan`
  - `getValueChan`
  - `resetTimeoutChan`
  - `setHeartBeatChan`
  - `voteRespChan`
  - `appendRespChan`

## 4. 当前并发模型

按照 PDF 要求，当前实现没有使用 `mutex`。

采用的是：

- 单 goroutine `eventLoop()` 作为核心状态机
- RPC handler 不直接修改共享状态
- RPC handler 只把请求封装后发送到 channel
- `eventLoop()` 统一接收消息并更新状态

这部分已经满足“禁止使用 mutex、使用 channel 组织并发”的要求。

## 5. 当前已完成逻辑

### 5.1 节点启动

已完成：

- 解析命令行参数
- 建立节点端口映射
- 启动 gRPC server
- 连接其他节点 client
- 启动主事件循环

### 5.2 Leader Election

已完成：

- follower 超时后发起选举
- candidate 自增 term
- candidate 给自己投票
- 并发发送 `RequestVote`
- 统计投票
- 获得多数票后成为 leader
- leader 当选后立即发送空 `AppendEntries`
- leader 定期发送 heartbeat
- 收到更高 term 后回退为 follower
- `SetElectionTimeout` 可动态修改超时
- `SetHeartBeatInterval` 可动态修改心跳间隔

### 5.3 RequestVote

已完成：

- 小 term 拒绝
- 大 term 更新本地 term 并回退为 follower
- 同 term 下只在未投票或重复投给同一候选人时允许投票
- 比较日志新旧：
  - 先比 `LastLogTerm`
  - 再比 `LastLogIndex`

### 5.4 AppendEntries

已完成：

- 小 term 拒绝
- 大 term 更新 term 并跟随 leader
- 更新 `currentLeaderId`
- 重置 follower 选举时钟
- 检查 `PrevLogIndex` / `PrevLogTerm`
- 冲突时截断日志
- 补齐缺失日志
- 按 `LeaderCommit` 推进 follower 的 `commitIndex`
- 调用 `applyCommittedEntries()` 把已提交日志作用到本地 KV

### 5.5 Propose

已完成的部分：

- follower 收到 `Propose` 时返回 `WrongNode`
- leader 收到 `Propose` 时把操作追加到本地日志
- 并发向 followers 发送日志复制
- 使用 `pendingPropose` 暂存未完成提交的提案
- 在后续 heartbeat 轮次中推进 commit 并唤醒等待的 `Propose`

### 5.6 GetValue

已完成：

- 通过 channel 进入事件循环读取本地 KV
- 存在返回 `KeyFound`
- 不存在返回 `KeyNotFound`

## 6. 当前测试结果

已通过 8/10：

- `testOneCandidateOneRoundElection`
- `testOneCandidateStartTwoElection`
- `testTwoCandidateForElection`
- `testSplitVote`
- `testAllForElection`
- `testLeaderRevertToFollower`
- `testOneSimplePut`
- `testOneSimpleUpdate`

未通过 2/10：

- `testOneSimpleDelete`
- `testDeleteNonExistKey`

## 7. 当前未解决的关键问题

剩余问题集中在 delete 的提交时序，不是基本复制失败。

### 7.1 `testOneSimpleDelete`

现象：

- 老师测试要求：
  - leader 先在某个时刻仅提交 `put`
  - 之后再在更晚时刻提交 `delete`
- 当前实现中：
  - leader 对“真实 delete”的本地提交时机仍然和 grader 期望不完全对齐

直接表现为：

- 某个检查点上，leader 或 follower 对 key 的可见状态过早变化或过晚变化

### 7.2 `testDeleteNonExistKey`

现象：

- 删除一个不存在的 key 时，测试依然要求 delete 作为日志项复制并提交
- 但这个 delete 不应该让已有 key 的可见状态提前变化
- 当前实现中，`KeyNotFound` 的返回与 commit 推进时机已经部分接近，但 follower/leader 某些时点仍然提前看到了 put 的结果

## 8. 当前已尝试过的修正方向

已经做过这些调整：

- 让 `Propose` 不再立刻完成，而是等待后续 heartbeat 驱动提交
- 区分“本地提交”和“后续将 `LeaderCommit` 广播给 followers”的两个时刻
- 把心跳发送时间改为在 `Propose` 广播之后重新计时
- 增加 `nextCommitTarget()`，尝试让 leader 不要一次性错误推进到过深的 commit

说明：

- 当前系统已经不是“完全没实现”
- 剩下的 2 个 case 是更细粒度的时序一致性问题

## 9. 下一步建议

下一步应聚焦两件事：

1. 把“leader 本地提交”和“followers 接收到更高 `LeaderCommit`”严格拆成两个阶段。
2. 对 delete 类提案增加更严格的提交规则，尤其是：
   - delete 之前是否已有 committed put
   - delete 的返回时机
   - delete 对本地 KV 的真正生效时机

## 10. 风险提醒

当前实现的主要风险：

- `eventLoop()` 里同时承载了选举、复制、提交和提案完成逻辑，后续修 delete 时序时需要避免破坏已通过的 8 个测试。
- `broadcastAppendEntries()` 是基于当前 `nextIndex` 状态发送日志，delete 修复时如果不小心调整 commit/heartbeat 顺序，容易导致回归。
