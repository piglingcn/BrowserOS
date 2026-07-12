import { describe, expect, it } from 'bun:test'
import { buildDouyinWorkflowPrompt } from './douyin-workflow'

describe('buildDouyinWorkflowPrompt', () => {
  it('asks the assistant to extract the keyword and defaults to 50 results', () => {
    const prompt = buildDouyinWorkflowPrompt('帮我采集美食探店视频')

    expect(prompt).toContain('用户需求：帮我采集美食探店视频')
    expect(prompt).toContain('searchKeyword：要搜索的视频关键词')
    expect(prompt).toContain('用户没有指定数量时固定使用 50')
    expect(prompt).toContain('<URL编码后的searchKeyword>?type=video')
    expect(prompt).toContain('不要点击“视频”标签')
    expect(prompt).toContain('不要再次填写搜索框')
    expect(prompt).toContain('可见文字为“筛选”的按钮 ref')
    expect(prompt).toContain('act(kind="click", ref=该ref)')
    expect(prompt).toContain('确认页面或筛选面板显示“最多点赞”已选中')
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
