export function buildDouyinWorkflowPrompt(
  request: string,
  includeDiagnostics = true,
): string {
  const diagnosticsSection = includeDiagnostics
    ? `

调试输出：
- 任务结束后，额外输出“工作流诊断记录”
- 记录本次最终成功的最短操作路径
- 逐步记录使用的工具、目标元素语义和判断成功的依据
- 记录失败尝试、失败原因、重试次数和恢复方式
- 记录搜索页、筛选面板、详情页及底部互动栏实际观察到的稳定特征
- 记录候选数量、详情页成功数、重试成功数、最终失败数及失败原因
- 记录点赞、评论、收藏、转发四项数据的实际定位和对应依据
- 最后列出建议加入下一版提示词的明确规则
- 不记录 Cookie、Token、签名参数、个人信息或完整页面源码`
    : ''

  return `请执行抖音热门视频数据采集任务。

用户需求：${request.trim()}

开始操作前，先从用户需求中提取：
- searchKeyword：要搜索的视频关键词
- targetCount：要采集的视频数量；用户没有指定数量时固定使用 10
- searchUrl：https://www.douyin.com/search/<URL编码后的searchKeyword>?type=video&sort_type=1&publish_time=0
- 先确认以上三个值，再开始浏览器操作

范围限制：
- 只采集结构化数据，不获取或保存视频文件
- 不读取 video.src，不处理媒体 Blob，不执行 curl，不返回 base64
- 固定采集排序后的前 targetCount 个视频，不要超过 targetCount 个
- 一次只处理一个视频；当前视频记录完成后再处理下一个
- 禁止把所有记录保存在 results 数组或其他内存集合中，必须获取一条就写入文件一条
- 遇到登录页、验证码或风控验证时立即暂停，请用户手动处理

执行步骤：
1. 在当前活动页直接 navigate/goto 到 searchUrl，禁止使用 new_page 为搜索页新开标签。等待视频搜索结果加载完成并确认用户已登录。searchUrl 已通过 sort_type=1 指定“最多点赞”，不要打开抖音首页，不要点击“视频”或“筛选”，不要判断筛选面板状态，也不要再次填写搜索框。
2. 将这个当前搜索结果页的 page ID 记录为 listPage，整个采集过程不得关闭或在该页直接打开视频详情。使用 run/evaluate 从 listPage 的 DOM 中提取 href 包含 /video/ 的链接，转换成绝对地址并去重。标题优先读取链接附近的可见文字、aria-label 或 title。
3. 如果当前可见 DOM 不足 targetCount 条，始终在 listPage 中缓慢向下滚动一屏，等待新结果出现后再次提取；只保留页面顺序中的前 targetCount 个唯一链接。候选列表达到 targetCount 条后停止滚动。
4. 在处理详情前创建 Markdown 结果文件 douyin-top<targetCount>-<关键词>-<日期>.md，立即写入标题、关键词、采集时间、排序依据和表头。如果当前会话没有可追加写入文件的工具，必须在打开任何详情页之前停止并报告，禁止把全部结果暂存在对话或内存中。
5. 再按候选列表顺序逐条补齐数据。每条记录：
   - 序号
   - 视频名称
   - 视频链接
   - 点赞数
   - 评论总数
   - 收藏数
   - 转发数
6. 每个候选视频都必须单独处理：调用 new_page(url=候选链接) 在新标签页打开详情并保存返回的详情 page ID，再调用 show_page(page=详情页ID) 明确切换到该页；等待详情加载后 snapshot，一次读取底部互动栏中带有“点赞、评论、收藏、分享/转发”语义的四个可见数值。
7. 当前视频读取完成后，立即向结果文件追加一行 Markdown 表格记录并确认写入成功。某个字段读取不到时填 null，但仍写入该视频；禁止等待全部视频完成后统一写入，禁止维护 results 数组。
8. 单行写入成功后立即清除当前视频的临时字段，调用 close_page(page=详情页ID) 立刻关闭当前详情标签页，再调用 show_page(page=listPage) 回到原搜索结果页并确认详情页已关闭，然后才允许处理下一个候选视频。禁止在详情页直接跳转到下一条，禁止同时打开多个详情页，任何时刻最多只能存在 listPage 和一个详情页。

数据提取要求：
- 页面点击必须使用 snapshot 返回的最新 ref 和 act，不要猜坐标
- 搜索页必须复用任务开始时的当前活动页；仅视频详情允许 new_page，且每条处理后必须立即 close_page
- 固定执行顺序为：直接打开带 sort_type=1 的视频搜索地址 -> 建立候选列表 -> 逐条采集
- 优先使用 run/evaluate 一次返回当前页面的结构化 JSON，避免反复截图和大段 DOM
- 每条详情固定执行：new_page -> 读取单条 -> 追加写入文件 -> 清除单条临时数据 -> close_page -> show_page(listPage)，确认写入和返回列表后再继续
- 数量字段同时保留页面原始文本和可解析的整数值；无法确认时填 null，不要猜测
- 候选列表建立后先报告“已找到 N/targetCount 条”；采集过程中定期报告简短进度
- 必须保持“最多点赞”排序后的页面顺序，不要自行重新排序

结果文件：
- 使用步骤 4 已创建的 Markdown 文件 douyin-top<targetCount>-<关键词>-<日期>.md，不要在任务结束时重新生成整个文件
- 文件标题写“抖音热门视频 Top <targetCount>”，并注明关键词、采集时间和排序依据“最多点赞”
- 使用 Markdown 表格，列顺序固定为：序号、视频名称、视频链接、点赞数、评论数、收藏数、转发数
- 视频名称作为链接文字，格式为 [视频名称](视频链接)
- 表格后列出字段缺失或采集失败的视频及原因
- 任务结束时只报告文件路径、成功条数、失败条数和缺失字段摘要；禁止在对话中再次输出完整 Markdown 表格${diagnosticsSection}`
}
