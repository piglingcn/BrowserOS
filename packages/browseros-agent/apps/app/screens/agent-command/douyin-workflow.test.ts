import { describe, expect, it } from 'bun:test'
import { buildDouyinWorkflowPrompt } from './douyin-workflow'

describe('buildDouyinWorkflowPrompt', () => {
  it('asks the assistant to extract the keyword and defaults to 10 results', () => {
    const prompt = buildDouyinWorkflowPrompt('帮我采集美食探店视频')

    expect(prompt).toContain('用户需求：帮我采集美食探店视频')
    expect(prompt).toContain('searchKeyword：要搜索的视频关键词')
    expect(prompt).toContain('用户没有指定数量时固定使用 10')
    expect(prompt).toContain(
      '<URL编码后的searchKeyword>?type=video&sort_type=1&publish_time=0',
    )
    expect(prompt).toContain('不要再次填写搜索框')
    expect(prompt).toContain('不要判断筛选面板状态')
    expect(prompt).toContain('不要点击“视频”或“筛选”')
    expect(prompt).toContain('searchUrl 已通过 sort_type=1 指定“最多点赞”')
    expect(prompt).toContain('在当前活动页直接 navigate/goto 到 searchUrl')
    expect(prompt).toContain('禁止使用 new_page 为搜索页新开标签')
    expect(prompt).toContain('href 包含 /video/ 的链接')
    expect(prompt).toContain('前 targetCount 个唯一链接')
    expect(prompt).toContain('记录为 listPage')
    expect(prompt).toContain('new_page(url=候选链接)')
    expect(prompt).toContain('show_page(page=详情页ID)')
    expect(prompt).toContain('close_page(page=详情页ID)')
    expect(prompt).toContain('show_page(page=listPage)')
    expect(prompt).toContain('禁止同时打开多个详情页')
    expect(prompt).toContain('字段读取不到时填 null')
    expect(prompt).toContain('点赞数')
    expect(prompt).toContain('评论总数')
    expect(prompt).toContain('收藏数')
    expect(prompt).toContain('转发数')
    expect(prompt).toContain('douyin-top<targetCount>-<关键词>-<日期>.md')
    expect(prompt).toContain('视频名称作为链接文字')
    expect(prompt).toContain('必须获取一条就写入文件一条')
    expect(prompt).toContain('在处理详情前创建 Markdown 结果文件')
    expect(prompt).toContain('立即向结果文件追加一行 Markdown')
    expect(prompt).toContain('禁止维护 results 数组')
    expect(prompt).toContain('清除当前视频的临时字段')
    expect(prompt).toContain('立刻关闭当前详情标签页')
    expect(prompt).toContain('任何时刻最多只能存在 listPage 和一个详情页')
    expect(prompt).toContain('仅视频详情允许 new_page')
    expect(prompt).toContain('禁止在对话中再次输出完整 Markdown 表格')
    expect(prompt).not.toContain('单列')
    expect(prompt).not.toContain('currentSrc')
    expect(prompt).not.toContain('分片下载')
    expect(prompt).not.toContain('comments.jsonl')
    expect(prompt).toContain('工作流诊断记录')
    expect(prompt).toContain('失败尝试、失败原因、重试次数和恢复方式')
    expect(prompt).toContain('不记录 Cookie、Token、签名参数')
  })

  it('passes an explicit result count to the assistant', () => {
    const prompt = buildDouyinWorkflowPrompt('美食探店，采集前20条')

    expect(prompt).toContain('用户需求：美食探店，采集前20条')
    expect(prompt).toContain('targetCount：要采集的视频数量')
  })

  it('can disable diagnostics for production use', () => {
    const prompt = buildDouyinWorkflowPrompt('美食探店', false)

    expect(prompt).not.toContain('工作流诊断记录')
  })
})
