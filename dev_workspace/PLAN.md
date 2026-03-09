# Raft Assignment 工作拆解

## 1. 当前工程结构

- `yourCode/main.go`
  - 唯一需要提交并实现的核心文件。
  - 目前只有 gRPC 服务骨架，Raft 逻辑基本未实现。
- `raft.proto`
  - 定义了所有 RPC、日志结构、状态枚举。
- `tests/rafttest/leaderElectionTest.go`
  - 前 6 个测试主要覆盖选主行为和事件顺序。
- `tests/rafttest/logReplicationTest.go`
  - 后 4 个测试覆盖日志复制、提交、删除和错误返回。
- `scripts/rafttest.sh`
  - 全量测试脚本。
- `scripts/rafttest_single.sh`
  - 单测脚本。

## 2. PDF 中最关键的硬性要求

- 最终代码写在 `yourCode/main.go`。
- 不要改文件名，否则可能直接 0 分。
- 只允许使用标准库。
- Go 版本实现里禁止使用 `mutex` 及相关同步组件。
- 需要使用 `channel` 组织并发逻辑。
- 不需要实现：
  - 随机 election timeout
  - snapshot / log compaction
  - membership change
  - 磁盘持久化
- log 下标从 1 开始理解，不是 0。
- 成为 leader 后要立刻发一次空的 `AppendEntries` 宣布当选。
- follower 也要能响应 `GetValue`。
- `Propose` 发到 follower 时，要返回 `WrongNode` 和当前 leader。
- 删除不存在 key 的判断时机要和日志提交时机严格对应。

## 3. 需要实现的核心模块

### A. 节点状态

在 `raftNode` 中至少需要补齐这些状态：

- 节点身份：`Follower` / `Candidate` / `Leader`
- 当前任期 `currentTerm`
- `votedFor`
- 日志数组 `log`
- 已提交下标 `commitIndex`
- 当前已知 leader `currentLeader`
- 内存 KV 存储
- 其他节点 RPC client 映射
- `nextIndex` / `matchIndex`（leader 复制日志时需要）
- 选举超时和心跳间隔
- 用 channel 驱动的事件循环/状态更新机制

### B. 选主流程

需要实现：

- 节点启动后先是 follower
- election timeout 到期后发起选举
- 自增 term，给自己投票
- 并发发送 `RequestVote`
- 收到多数票后成为 leader
- 立刻发送空 `AppendEntries`
- leader 周期性 heartbeat
- 遇到更高 term 时退回 follower

### C. `RequestVote`

需要处理：

- 请求 term 小于本地：拒绝
- 请求 term 更大：更新 term，退回 follower
- 每个 term 只能投一次
- 比较候选者日志是否至少和自己一样新
- 返回正确的 `Term` 与 `VoteGranted`

### D. `AppendEntries`

需要处理：

- term 过旧直接拒绝
- term 更新时切换为 follower
- 重置选举超时
- 检查 `PrevLogIndex` / `PrevLogTerm`
- 冲突日志截断并追加新日志
- 用 `LeaderCommit` 推进提交
- 提交后立即应用到内存 KV
- 返回 `Success` 和 `MatchIndex`

### E. `Propose`

leader 上需要：

- 把操作追加到本地日志
- 并发复制给 followers
- 多数成功后再提交
- 提交成功后返回：
  - `Put` 成功：`OK`
  - 删除存在 key：`OK`
  - 删除不存在 key：`KeyNotFound`

follower 上需要：

- 返回 `WrongNode`
- 附带 `CurrentLeader`

### F. `GetValue`

- 直接查本地内存 KV
- 存在返回 `KeyFound`
- 不存在返回 `KeyNotFound`

### G. 定时接口

- `SetElectionTimeout`
- `SetHeartBeatInterval`

两者都要能在运行时重置对应定时机制。

## 4. 推荐实现顺序

1. 先补 `raftNode` 结构和统一事件循环。
2. 实现 `SetElectionTimeout` / `SetHeartBeatInterval`。
3. 只做 leader election，先通过前 6 个测试。
4. 再补日志复制字段和 `AppendEntries` 日志一致性。
5. 实现 `Propose` 提交逻辑。
6. 最后处理 delete 的边界时序和 `KeyNotFound`。

## 5. 测试推进建议

- 先跑：
  - `bash ../scripts/rafttest_single.sh testOneCandidateOneRoundElection`
- 选主稳定后再跑：
  - `bash ../scripts/rafttest.sh`
- 每过一个测试都要回归，避免 regression。

## 6. 新工作目录说明

这个目录仅用于：

- 分析文档
- 辅助脚本
- 独立虚拟环境

注意：最终可提交实现仍然必须回到 `yourCode/main.go`。

## 7. 虚拟环境

已创建：

- `dev_workspace/.venv`

激活方式：

```bash
source dev_workspace/.venv/bin/activate
```

说明：

- 该作业主实现语言是 Go，这个虚拟环境主要用于你写辅助分析脚本或本地工具，不替代 Go 的编译与测试环境。

## 8. 额外提醒

PDF 明确写了：`Using AI coding tools is NOT allowed.`

如果你后续要继续让我直接代写可提交代码，需要你自己确认这是否符合课程规定。
