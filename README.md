# 数字微波链路规划后端

Go 1.22 + Gin + MySQL 8.0 的分层设备参数、链路预算、降雨可用度和重算任务服务。

## 快速启动

```bash
docker compose up --build
# API: http://localhost:8080/api/v1
```

服务启动时自动建表。后台 worker 每 1 秒轮询任务队列表；HTTP 只暴露 API，不暴露 MySQL。

## 核心规则

1. **三层参数**
   - `model`：型号标称层。scope 为电台型号 ID 或天线型号 ID。
   - `batch`：批次修正层。scope 为批次 ID。
   - `site`：站点覆盖层。scope 为站点 ID。
2. 下层版本只包含需要覆盖的字段；未出现的字段按固定优先级继承。
3. 草稿可反复修改，发布生成不可变版本：`model.radio-x.v000001`。
4. 合并结果是纯函数：只取决于端点引用和一组显式版本号，不依赖录入时间或版本排序。
5. 每次评估保存完整拓扑、有效参数、预算结果和全部版本引用，可按历史版本原样复现。

合并优先级：

```text
电台字段: model(radio)  ->  batch  ->  site(仅安装字段)
天线字段: model(antenna)
```

站点层允许 `feeder_length_m`、`splitter_loss_db`、`splitter_count`；批次层允许修正电台标称字段。字段删除后，新版本不包含该字段，因此自动回落到批次或型号层。

## 链路预算

自由空间损耗采用 ITU-R P.530：

```text
Lfs(dB) = 92.45 + 20log10(f_GHz) + 20log10(d_km)
```

18 GHz、10 km 为约 `137.55 dB`；距离或频率加倍增加 `20log10(2)=6.0206 dB`。

单方向接收电平：

```text
Pr = Pt - L_tx_feeder/splitter + G_tx - Lfs + G_rx - L_rx_feeder/splitter
fade_margin = Pr - receiver_threshold
```

服务同时计算 A→B、B→A，并以较小衰落余量用于可用度。接收门限高于“零传播距离理论最大接收电平”时返回 warning，不作为请求错误。

## 降雨衰减与可用度

- 雨强：气候区保存 `R0.01`，即平均年 0.01% 时间超过的 1 分钟积分降雨率（mm/h）。
- 比衰减：ITU-R P.838-3，`γ_R = k R0.01^α`，`k,α` 按频段和极化维护。
- 地面视距链路有效路径长度、0.001%–1% 时间换算：ITU-R P.530-17 第 2.4 节（式 32–35）。
- 输出 `p` 为一年中雨衰超过衰落余量的时间百分比，`availability = 1 - p/100`。

雨强变大时，固定余量对应的超过概率不减，因此可用度不升。`internal/rain` 使用二分法反解百分比，避免直接近似求根。

## 多跳通道合成

服务同时给出两种口径：

1. **独立口径（验收口径）**：`A = Π A_i`，实现简单，不把同一场雨造成的重叠停机重复计时之外做任何抵扣，因此在相邻降雨正相关时偏保守。
2. **相邻相关口径（诊断口径）**：一阶 Gaussian copula，默认相邻潜变量相关系数 `ρ=0.5`。相邻 hop 用二元正态联合分布计算共同 outage；非相邻 hop 沿 Markov 链传递。copula 方法见 Nelsen, *An Introduction to Copulas*。ITU-R P.530 规定单跳雨衰，并不强制规定多跳相关模型；因此这里明确把相关模型作为规划参数。

三跳、每跳年 outage 均为 0.01%、ρ=0.5 的算例：

- 独立 outage：0.029997%，可用度 99.970003%。
- 相关 outage：0.029533%，可用度 99.970467%。
- 独立口径比相关口径多估 outage `0.000464` 个百分点，即可用度低约 `0.000464` 个百分点。
- ρ→1 时三跳退化为同一事件，outage 约 0.01%；独立口径仍为约 0.03%，即独立口径把 outage 放大约 3 倍。

通道是否达标默认按更保守的独立口径判断；相关口径用于解释同场雨影响。

## 重算与影响分析

发布接口在一个数据库事务中写入：新版本、发布后版本快照、重算任务。任务启动后固定 `start_refs`：

- 任务运行期间发布的版本不会改变正在运行任务的输入。
- 新版本有自己的队列任务；任务结束后若发现仍有更新未处理，会追加一个 follow-up。
- 影响清单先按 scope 关系过滤，再比较发布前后有效预算；只改无关型号不会列入，也不会改变其评估。
- 任务结果列出余量下降链路、已评估通道、低于目标通道。

## 草稿冲突

`PUT /api/v1/drafts` 使用 `expected_revision` 乐观锁：

1. 先 `GET /drafts/{layer}/{scope}` 获取 revision。
2. 保存时带回；revision 已变化返回 HTTP 409。

## 主要 HTTP 接口

### 基础数据

- `GET/POST /radios`、`GET /radios/{id}`
- `GET/POST /antennas`、`GET /antennas/{id}`
- `GET/POST /sites`、`GET /sites/{id}`
- `GET/POST /batches`、`GET /batches/{id}`
- `GET/POST /climate-zones`
- `GET/POST /rain-bands`

### 参数与版本

- `GET /drafts/{layer}/{scope}`
- `PUT /drafts`：`{layer, scope_id, upserts, deletes, expected_revision}`
- `POST /publish`：发布并创建重算任务，HTTP 202 返回版本和任务号
- `GET /versions/{version}`，版本号不含 `/`，例如 `model.r1.v000001`
- `GET /versions/{layer}/{scope}`
- `POST /effective`：返回合并值、字段来源和版本号

### 拓扑与评估

- `GET/PUT/DELETE /links/{id}`
- `POST /links/{id}/evaluate`
- `GET/PUT/DELETE /channels/{id}`
- `POST /channels/{id}/evaluate`
- `GET /jobs`、`GET /jobs/{id}`、`POST /jobs/{id}/run`（测试/手动推进；生产由 worker 自动执行）
- `GET /assessments?subject_id=...`
- `GET /assessments/{id}`
- `POST /assessments/{id}/replay`
- `GET /assessments/compare?left={id}&right={id}`

## 校验错误

错误响应形如：

```json
{
  "error": "invalid input",
  "fields": [{"field": "a.frequency_mhz", "message": "frequency is outside this radio model's supported band"}]
}
```

覆盖：频率不在电台频段、距离非正、增益/损耗非有限数、引用不存在型号/批次/站点/版本、通道相邻链路不共享站点。门限高于理论最大接收电平只出现在 `warnings`。

## 本地测试

```bash
go test ./...
```

测试覆盖需求列出的全部性质：FSPL 参考值与加倍关系、录入顺序无关、来源标注、删字段回落、两端互换、雨强单调、两种通道合成口径、精确影响清单、任务期间发布、历史复现和草稿冲突。
