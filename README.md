# 微波传输网分层参数与链路预算服务

面向省干/本地数字微波网络规划的后端服务：管理三层设备参数（型号标称、批次修正、
站点覆盖），在此基础上做链路预算、降雨可用度评估、多跳通道合成，并在参数发布后
精确找出受影响的链路/通道进行后台重算与对比。

- Go 1.22 + Gin，数据存 MySQL 8.0（仅暴露 HTTP 接口）
- 纯领域逻辑（分层合并、链路预算、降雨、通道合成、影响分析）与存储、HTTP 解耦，
  全部用 Go 标准 `testing` 覆盖
- 每次评估冻结其依据的全部参数版本，任意历史评估可原样复现

## 1. 包结构

```
internal/params       三层参数模型与确定性合并、字段来源标注、有效参数哈希
internal/num          有限数判定等小工具
internal/linkbudget   自由空间损耗、两端馈线/分路/额外损耗、收电平与衰落余量
internal/rain         ITU-R 降雨衰减与年超越时间百分比
internal/channel      多跳通道可用度合成（独立 / 相邻相关 / 保守界三口径）
internal/impact       精确影响传播（端点→链路→通道）与预算相关字段判定
internal/version      草稿乐观锁与发布版本规则
internal/model        持久化实体与评估/任务数据结构
internal/store        MySQL 存储、发布+影响+快照冻结事务、版本化读取
internal/assessment   单跳/通道即时评估、冻结版本评估、历史复现
internal/task         后台重算队列、任务执行、任务期间新版本的追加
internal/httpapi      Gin 路由、字段级校验报错
cmd/server            进程入口
```

## 2. 三层参数与确定性合并

三层自低到高：

| 层 | scopeKind | scopeId | 含义 |
|---|---|---|---|
| `nominal` | `radio_model` / `antenna_model` | 型号 ID | 厂家标称 |
| `batch` | `radio_batch` / `antenna_batch` | 批次 ID | 实测批次修正 |
| `site` | `endpoint_radio` / `endpoint_antenna` | 端点 ID | 站点安装条件覆盖 |

每层有全局单调递增版本号；**发布即只读**。下层只写要改的字段，未写的字段沿
`nominal → batch → site` 继承。合并规则在 `params.MergeFields` 中固定：

1. 层按 `LayerOrder` 常量的唯一顺序遍历；
2. 每层字段名先排序再写入；
3. 高层已有的同名字段整体覆盖低层。

因此同一组三层配置，无论草稿以什么顺序录入、map 迭代序如何，合并值与字段来源都
逐字节一致；这一性质有哈希测试和录入顺序测试固定。

每个有效字段都带来源：`{layer, version, setId}`。站点层删除一个字段（新发布的
版本不再包含它）后，有效值自动回落到批次层，批次层也没有则回落到标称层——回落
测试固定该行为。

草稿用乐观锁：保存时携带 `baseRevision`，两人改同一层同一 scope，后保存者收到
`409 draft_conflict`，内容包含对方当前 revision，要求重新读后再改。

## 3. 链路预算

双向都计算，结果取较差方向（保证 A/B 两端互换结果不变）：

```
Pr(dBm) = Ptx + Gt + Gr - Lt - Lr - Lfs
Lfs(dB) = 92.45 + 20log10(d/km) + 20log10(f/GHz)
M(dB)   = Pr - Pth
```

- 自由空间损耗依据自由空间传播公式，18 GHz/10 km 算得 **137.56 dB**；
  距离或频率加倍各 +20log10 2 ≈ **6.02 dB**（测试固定参考值与加倍关系）。
- 本端损耗 = 馈线长度 × 馈线单位损耗（标称，可被批次覆盖）+ 分路器损耗 +
  站点额外损耗。
- 校验（字段级报错，见 §8）：频率必须落在电台型号声明的频段内、距离为正、
  增益/损耗为有限数、引用的型号/版本必须存在。
- 收门限高于 `Ptx+Gt+Gr−Lt−Lr`（**不含**自由空间损耗的理论最大收电平）时
  返回 **warning 而非报错**：此时余量必然为负，但预算仍可计算。

## 4. 降雨衰减与可用度

计算与换算均采用 ITU-R 建议书的通行做法，并随评估记录方法出处：

1. **比衰减** γR = k·R^α（dB/km）：**ITU-R P.838-3**，k、α 按频段和极化
   （H/V）在 `rain_band_coefficients` 表维护。
2. **路径缩减** d_eff = d/(1+d/d0)，d0 = 35·exp(−0.015·R0.01)：
   **ITU-R P.530-17 §2.2.4.1**。
3. **0.01% 衰减** A0.01 = γR·d_eff。
4. **时间百分比换算** A_p/A0.01 = C1·p^(−(C2+C3·log10 p))，p∈[0.001%,1%]，
   C0=0.12+0.4·[log10(f/10)]^1.22，C1/C2/C3 为 P.530-17 式(10)–(13)：
   **ITU-R P.530-17 §2.2.4**。
5. 年超越百分比 p 由对 `A(p) = M`（衰落余量）二分求解；可用度 A = 1−p/100。
   解超出 0.001%–1% 标准区间时钳位到边界并标记 `rainMethodInValidRange=false`。

R0.01（1 分钟降雨率，mm/h，年超越 0.01%）按气候区在 `rain_climates` 表维护，
对应 **ITU-R P.837** 的降雨率统计口径。单调性：A(p) 随 p 单调下降，故 R0.01
变大 ⇒ 相同余量下 p 不减、可用度不升（单调测试固定）。

## 5. 多跳通道合成：口径选择与算例

先澄清一个常见直觉上的反直觉点。通道中断定义为“**至少一跳中断**”（串联业务，
任一跳断则通道断），多跳年不可用度是各跳中断事件之**并**的概率：

- 同一场雨同时压住相邻跳，是把多个中断事件“对齐”成同一时段，**并集反而更小**。
  对“任一中断”这种并集而言，正相关使各跳独立相乘口径**偏悲观（高估不可用度）**，
  而不是偏乐观。
- 真正的**乐观下界**是各跳完全正相关（同生共灭，不可用度 = 最大单跳）；
  **保守上界**是并集的 Fréchet 界 Σpᵢ（不假设任何相关性，恒成立）。
- 因此“独立相乘简单但偏乐观”这一说法只在“**所有跳同时可用**”（交集）口径下
  成立；对串联通道通常采用的“任一跳断”口径并不成立。

本服务同时给出三种口径，并选定**保守上界 Σpᵢ 作为规划判定口径**（对目标可用度
做 pass/fail），把相邻相关模型作为物理参考值一并返回。

**相邻相关模型（一阶 Markov + Fréchet 混合）**：只假设物理相邻跳相关，非相邻
跳条件独立；相邻跳的联合中断概率用独立与完全正相关的凸混合

```
P(Ei ∩ Ei+1) = (1−ρ)·pᵢ·pᵢ₊₁ + ρ·min(pᵢ,pᵢ₊₁),   ρ∈[0,1]（通道上可配，默认0.6）
U_n = U_{n−1} + p_n − P(E_n ∩ E_{n−1})      （一阶 Markov，只记前一跳）
```

ρ=0 退化为独立；ρ=1 相邻跳完全同生共灭（此时 n 跳链 Uₙ=Σp−(n−1)·min(p,…)）。
ρ 是规划参数，不是某个建议书的常数；它对应工程上“雨区同时覆盖相邻段”的相关
强度，相关结构参考 ITU-R P.530 分集/联合衰落处理与共站址共雨区的常识假设。

**具体算例（3 跳，每跳年不可用度 p = 0.01% = 1e-4，ρ = 0.6）：**

| 口径 | 年不可用度 | 年可用度 |
|---|---|---|
| 独立相乘 `1−(1−p)³` | 2.9997×10⁻⁴（0.029997%） | 0.99970003 |
| 相邻相关（本模型） `3p−2·[(1−ρ)p²+ρp]` = 3e−4−1.2e−4 | **1.79992×10⁻⁴（0.017999%）** | **0.99982001** |
| 保守界 Σpᵢ（**判定口径**） | 3.0×10⁻⁴（0.03%） | 0.99970000 |

相对量化：

- 独立相乘相比相邻相关模型，把不可用度**高估**约 1.2×10⁻⁴（约 0.012 个百分点/年，
  约 1.05 小时/年），即独立值是相关估计的 **1.67 倍**——在“任一跳断”口径下，
  相乘并不偏乐观，偏的是保守方向。
- 相邻相关模型相比选定的保守界 Σpᵢ，把不可用度**低估** 1.2×10⁻⁴（约为界值的
  40%）。这正是判定时不采用它的原因：它依赖 ρ 的选取，而 ρ 难以从单站统计精确
  标定；保守界不依赖任何相关假设，永远成立。
- 若把目标定在 99.98%（允许 0.02%）：相关口径通过、保守界不通过——用保守界做
  判定会多预留约 1.05 小时/年的中断预算，这是为“同雨区相关参数不确定”付的
  保险费。

## 6. 发布、影响分析与后台重算

### 6.1 精确影响集合

发布事务（`store.PublishWithTask`，单事务）内完成：

1. 锁定草稿与该层 head 版本；
2. 按 scope 精确找出实际解析它的端点（标称→按型号列匹配、批次→按批次列匹配、
   站点→该端点），计算发布前有效参数数值哈希；
3. 写入新版本、推进该层全局版本；
4. 对同一批端点重算发布后哈希，**仅数值哈希变化的端点**进入受影响集合；
5. 受影响端点 → 其所在链路 → 这些链路所属通道，逐层精确传播；
6. 冻结受影响链路两端 **以及受影响通道中全部链路两端** 的六个参数集合版本
   （通道可用度要对整条串接计算）；
7. 插入排队重算任务。

因此：

- 改一个没有任何端点引用的型号 → 哈希集合为空 → 该链路不重算、不出现在受影响
  清单里（性质测试与 HTTP 冒烟固定）；
- 只改预算不使用的字段（`impact.BudgetFieldNames` 之外）也不产生影响；
- 站点层加/删一个字段，只影响实际安装在该站点的端点。

### 6.2 冻结版本与任务期间的新版本

任务记录 `baseline`（提交前各层版本）与 `target`（提交时冻结的各层 head）。
任务执行时只用 `task_endpoint_snapshots` 里每个端点冻结的六个具体集合版本
（`SetAtVersion` 精确读历史版本），**不读当前 head**。所以任务运行期间再发布
新版本：

- 正在跑的任务结果不变，仍对应它开始时的版本；
- 发布会按 §6.1 再生成一个针对新版本的自动重算任务（其快照冻结新版本）；
- 老任务结束时若发现当前 head 与其 target 不同，只在“没有更新的排队/运行任务”
  时补一个到最新状态的 follow-up（`followup_of` 串起来），避免重复堆叠；多次
  中间发布合并为一次补算。

### 6.3 对比输出

任务结束给出 `diff`：

- `marginDecreased` / `marginIncreased`：每条受影响链路衰落余量前后值与 ΔdB；
- `channelsCrossedBelowTarget` / `channelsRecovered`：通道可用度跌破/恢复目标值。

任务提交即返回任务号，`GET /recompute-tasks/:id` 查状态与进度（端点数/链路数/
通道数的完成计数）。

### 6.4 历史复现

每次评估的 `layerVersions` 同时记录：全局三层 head（`_heads`）和每个端点六个
集合的 `{kind, layer, version, setId}`；payload 还快照气候区与降雨系数。
`POST /link-evaluations/:id/replay` 用记录的精确版本号重新加载集合并重算，
对比衰落余量与降雨超越百分比（容差 1e-9），返回 `reproducible`。不可变集合若
被改动会立刻体现为不匹配。

## 7. 成立的性质（均有测试固定）

1. 改无关型号：链路评估不变，且不在受影响清单。
2. 站点层删字段：有效值回落到批次层/标称层。
3. 链路两端互换：预算（取较差方向）不变；输入哈希与端序无关。
4. 降雨率变大：可用度不升。
5. 同一配置录入顺序不同：合并结果一致（确定性遍历 + 排序）。
6. 自由空间损耗参考值与距离/频率加倍关系。
7. 通道两种口径的对比算例。
8. 受影响清单精确性；任务期间发布新版本的处理；历史复现；草稿冲突。

## 8. 输入校验（报错并指出字段）

| 情况 | 处理 |
|---|---|
| 频率不在电台型号支持频段 | 400，字段 `endA/endB.radio.frequencyBand` |
| 距离不为正 | 400，字段 `distanceKM` |
| 增益/损耗非有限数 | 400，指出具体字段 |
| 门限高于“Ptx−全部本地损耗+天线增益”理论最大收电平 | 200 + `warnings`，余量为负 |
| 通道相邻链路不共享站点 | 400，字段 `linkIds[i]` |
| 引用不存在的型号/批次/气候区/系数/版本 | 404/400，指出引用字段 |
| 通道为空、目标可用度越界、ρ 越界、极化非 H/V | 400，指出字段 |
| 草稿并发修改 | 409 `draft_conflict`（非报错拒绝，提示重新读） |

## 9. HTTP 接口（`/api/v1`）

```
# 三层草稿与发布
GET    /layers/:layer/scopes/:scopeKind/:scopeId/draft
PUT    /layers/:layer/scopes/:scopeKind/:scopeId/draft      # body: baseRevision, fields
DELETE /layers/:layer/scopes/:scopeKind/:scopeId/draft
POST   /layers/:layer/scopes/:scopeKind/:scopeId/publish    # 返回 published + recomputeTaskId
GET    /layers/heads
GET    /layers/:layer/scopes/:scopeKind/:scopeId/versions/:version

# 有效参数（带来源）
GET    /endpoints/:id/effective

# 站点 / 端点 / 链路 / 通道 / 气候区 / 降雨系数
POST PUT GET /sites[/:id]   POST GET /rain-climates[/:id]   POST /rain-coefficients
POST PUT GET /endpoints[/:id]   POST PUT GET /links[/:id]   POST PUT GET /channels[/:id]

# 即时评估
POST   /links/:id/evaluate      POST /channels/:id/evaluate

# 后台重算
POST   /recompute-tasks        GET /recompute-tasks[/:id]

# 评估历史、对比、复现
GET    /links/:id/evaluations       GET /link-evaluations/:id
POST   /link-evaluations/:id/replay
GET    /channels/:id/evaluations    GET /channel-evaluations/:id
```

### 最小上手序列

```bash
# 1) 发布型号标称
curl -X PUT $B/layers/nominal/scopes/radio_model/r1/draft -H 'Content-Type: application/json' \
  -d '{"baseRevision":0,"fields":{"radio.txPowerDbm":15,"radio.rxThresholdDbm":-85,
       "radio.minFrequencyGHz":17.5,"radio.maxFrequencyGHz":18.7,
       "radio.feederLossDbPerM":0.12,"radio.splitterLossDb":0}}'
curl -X POST $B/layers/nominal/scopes/radio_model/r1/publish -d '{}'
# 2) 站点覆盖：加长馈线 / 额外损耗
curl -X PUT $B/layers/site/scopes/endpoint_radio/e1/draft -H 'Content-Type: application/json' \
  -d '{"baseRevision":0,"fields":{"installation.feederLengthM":40,"installation.extraLossDb":1.5}}'
curl -X POST $B/layers/site/scopes/endpoint_radio/e1/publish -d '{}'
# 3) 即时评估；4) 查任务 diff；5) 历史复现
curl -X POST $B/links/l1/evaluate
curl     $B/recompute-tasks/<taskId>
curl -X POST $B/link-evaluations/<evalId>/replay
```

## 10. 运行

```bash
docker compose up --build
# app: http://localhost:8080 （MySQL 8.0 仅在 compose 内网，不对外发布端口）
```

本地无 Docker 时用环境变量：`MYSQL_DSN`、`HTTP_ADDR`（默认 `:8080`）。
首次启动自动建表（schema 通过 `//go:embed` 打进二进制）。

### 测试

```bash
go test ./...
# 需要真实 MySQL 的集成测试（默认指向 127.0.0.1:3306，无库时自动 skip）：
MW_TEST_MYSQL_DSN='root@tcp(127.0.0.1:3306)/' go test ./internal/task -v
```

## 11. 关键取舍说明

- 判定口径选保守界 Σpᵢ 而非相邻相关值：相关模型更贴近物理，但其结果依赖 ρ，
  ρ 难以从单站统计标定；Σpᵢ 恒成立、无需相关假设，适合规划红线。两种值都返回，
  报告里可同时看到“物理估计”和“红线判定”。
- 发布与快照冻结放在一个数据库事务：保证“正在跑的任务看到的版本”和“发布生效
  的版本”严格对应，杜绝并发发布时冻结到错误版本。
- 双向预算取较差方向：用最小代价保证 A/B 互换不变性，也避免只算一个方向时漏掉
  异厂商/异配置两端的非对称余量。
