# 供应商出站协议

日期：2026-10-27

状态：待实现。本文描述提案，不是当前行为。当前选路见 [Gateway routing and fallback](../../subsystems/gateway-routing.md)。

## 目标

Agent 继续只连 magpie。模型目录仍汇聚各家供应商。某一轮选定一家带 API key 的供应商后，出站协议由这家钉死：

- 入站协议与出站协议相同：正文原样送出，只替换模型名和这家的密钥。
- 入站协议与出站协议不同：用现有翻译，只译成这家钉死的那一种协议。

效果对齐 cc-switch 把该 agent 指到这家之后的正文，同时保留 magpie 的模型汇聚。

## 不做

- 不模仿任何 CLI 的 TLS 指纹，不用 JA3、JA4 或其它握手伪装让供应商把 magpie 认成官方客户端。
- 不改变订阅账号、插件 fetch、Codex ChatGPT 后端的路径。
- `outbound` 为空时，不改变今天的选路、透传和翻译。

## 保持不变

`outbound` 为空时，`attempt`、`usable`、`passthrough`、`translate` 与现在相同。Claude 仍优先走 Messages，OpenAI 自家模型仍优先走 Responses。已有的 `ModelAPI` 在供应商标为空时含义不变。

模型汇聚不变：目录、路由组、密钥轮换、亲和、失败后换下一家、用量记录仍在 magpie。换到下一家时，下一家用它自己的 `outbound`。

订阅共享不变。`Account != nil` 的供应商不读 `outbound`。Claude 订阅仍走本机 `claude` 进程，Codex 订阅仍走 ChatGPT 后端，插件仍走插件的 fetch。路由组里可以同时有订阅账号和 key 供应商，各走各的路径。

实现只使用 Go 的 HTTP 和 `providers.json`，不加操作系统分支。macOS、Linux、Windows 和 WSL 同一条路径。WSL 里的 agent 仍先连到本机网关，再由网关按供应商出站。

## 配置

`Provider` 增加字段 `outbound`，JSON 名 `outbound`。取值：

| 值 | 含义 |
| --- | --- |
| 空 | 自动，沿用今天的 `usable()` |
| `chat` | Chat Completions |
| `responses` | Responses |
| `anthropic` | Anthropic Messages |
| `gemini` | Gemini `generateContent` |

`auto`、`default` 在保存时写成空。`openai` 在保存时写成 `chat`。其它字符串，或这家没有该协议的 URL，保存时清掉，请求按自动处理。

一次请求的出站协议按这个顺序决定，函数名 `LeavesAs`：

1. `Account != nil`：忽略 `outbound`，返回「不进入新路径」。
2. 该模型已有 `ModelAPI`，且 `Base` 有对应 URL：用模型上的协议。仅当供应商的 `outbound` 非空时，这一步才进入新路径。`outbound` 为空时，`ModelAPI` 仍只影响今天的选路。
3. 供应商 `outbound` 非空，且 `Base` 有对应 URL：用 `outbound`。
4. 以上都没有：不进入新路径，`usable()` 与今天相同。

设置入口：

- `providers.json` 的 `outbound`。字段在 `Provider` 上，随现有备份和同步走，不另开同步通道。
- `magpie provider set <id> outbound=anthropic`。`chat`、`responses`、`gemini` 同样。`auto` 清空。没有对应 URL 时命令失败，不写入。
- 供应商编辑器增加同一选项。保存请求没带该字段时，保留已保存的值，避免一次普通保存清掉 CLI 写上的值。这与 `codex=as-sent` 的保留方式相同。

## 请求路径

`LeavesAs` 没有结果：`attempt` 从今天的第一行继续，包括 `joinToolRuns`、`chatCallIDs` 和之后的透传或翻译。

`LeavesAs` 有结果，且等于入站协议：在那些改写之前返回，调用 `directForward`。

`LeavesAs` 有结果，且不等于入站协议：在那些改写之前返回，调用现有 `translate`，目标只此一种协议。`usable()` 在 `LeavesAs` 有结果且该协议尚未被标成 unfit 时只返回这一种。Messages 优先、Responses 优先、以及端点拒绝后改走另一种协议，在这家供应商上不发生。该协议已被标成 unfit 时，`usable()` 返回空，不再改用 `Speaks()` 把钉死的协议松开。

下面这些仍在分叉之前执行，直连不绕过：模型解析、密钥和额度、路由组、脱敏、把努力程度 `off` 记成 `none`。

## 同协议直连

`directForward` 的正文只经过现有的 `rewriteModel`：替换 `model` 这一个字符串，字段顺序和其余字符保持原样。上游模型名用现有的 `UpstreamName`。

鉴权用现有的 `Sign` 和 `AuthHeaders`，换成这家的 key，并遵守这家已有的 `AuthHeader`。客户端带给 magpie 的密钥不转发。

客户端表明身份的头原样转发，头名比较不区分大小写：

- `User-Agent`
- `anthropic-version`、`anthropic-beta`
- `x-stainless-*`、`x-claude-code-*`
- Codex 的 `originator` 和 `x-codex-*`

直连不跑这些现有改写：推理强度、`cache_control`、thinking、工具 id、billing 行、beta 重写，以及 `forwardOnce` 里按厂商改正文的步骤。供应商上已经配置的 `headers` 和 `extraBody` 仍然生效。

响应按字节抄回。用量从旁路读取，不改字节，不跑 `chatTidy` 一类改写。失败状态仍交给现有的 `holdWriter`，因此在首包写出之前仍可以换密钥或换下一家。

`directSend` 自己组请求，不调用 `forwardOnce`。`forwardOnce` 会改正文。

## 跨协议

入站与钉死的协议不同时，用现有 `translate` 生成那一种协议的请求。供应商看到的是它自己的协议。Claude CLI 说不了 Chat 或 Responses，这一跳的字节与「把 `ANTHROPIC_BASE_URL` 指到这家」不同。

`translate` 里按 `usable()` 改协议的分支，在钉死之后只能看到这一种协议。`forwardTranslated` 在当前协议已被拒绝、下一候选仍是同一协议时，把该次拒绝返回，不反复请求同一端点。

## 与 cc-switch 直连的差距

同协议、且这家是 API key 时，供应商看到的正文是 agent 发出的正文，只换了模型名和密钥。

下面这些保持，并且各平台一样。它们不是本提案要补上的缺口：

- TCP 和 TLS 使用 Go 的栈。不模仿 CLI 的握手指纹。
- 头的名字和值对齐，顺序不保证。
- 脱敏仍然生效。要连正文里的秘密也原样出去，沿用现有的 `unmasked=yes`。
- 给客户端的响应仍带 `X-Magpie-Provider` 和 `X-Magpie-Model`。这两项不发给供应商。
- 第一家在首包前失败时，magpie 仍会换下一家。

## 测试

测试用 `httptest`，不访问外网，三个操作系统同一套 `go test`。

- `outbound` 为空：现有的 Claude 走 Messages、同协议透传测试保持通过。
- `outbound=anthropic`，入站 Messages：上游正文等于只改了 `model` 的原文；`anthropic-beta` 和 `User-Agent` 在；客户端带的 magpie 密钥不在。
- `outbound=chat`，入站 Messages：请求发到 Chat Completions 的路径，不发到 `/v1/messages`。
- 带订阅账号的供应商即使写了 `outbound`，仍走订阅桥，不进入 `directForward`。
- 路由组里一家钉了协议、另一家是订阅：各走各的路径。
- 编辑器保存不带 `outbound` 时，已保存的值还在。
- `outbound=chat` 但没有 Chat URL：CLI 失败；若直接写入文件，`normalize` 将其清掉。

## 实现时要碰到的位置

- `Provider.Outbound` 与 `normalize`：`internal/provider/provider.go`
- `LeavesAs`：与 `ModelAPI` 放在一起，`internal/provider/modelprefs.go`
- `attempt` 开头的分叉，以及 `usable()` 在钉死时只返回一种协议：`internal/gateway/gateway.go`
- `directForward` 与 `directSend`：新文件 `internal/gateway/direct.go`，避免再扩大 `passthrough`
- CLI：`internal/cli/providers_cli.go` 的 `outbound=`
- 编辑器的保留逻辑：`internal/gui/providers.go` 的 `providerJSON`

实现完成后，把本提案中已经落地的行为写进 [gateway-routing.md](../../subsystems/gateway-routing.md)。在那之前，该页仍只描述当前行为。
