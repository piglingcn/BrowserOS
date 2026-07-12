import { describe, expect, it } from 'bun:test'
import { buildDouyinWorkflowPrompt } from './douyin-workflow'

describe('buildDouyinWorkflowPrompt', () => {
  it('opens an encoded video search URL and visits each result in a temporary tab', () => {
    const prompt = buildDouyinWorkflowPrompt('美食 探店')

    expect(prompt).toContain(
      'https://www.douyin.com/search/%E7%BE%8E%E9%A3%9F%20%E6%8E%A2%E5%BA%97?type=video',
    )
    expect(prompt).toContain('用户搜索词：美食 探店')
    expect(prompt).toContain('不要点击“视频”标签')
    expect(prompt).toContain('不要再次填写搜索框')
    expect(prompt).toContain('可见文字为“筛选”的按钮 ref')
    expect(prompt).toContain('act(kind="click", ref=该ref)')
    expect(prompt).toContain('确认页面或筛选面板显示“最多点赞”已选中')
    expect(prompt).toContain('href 包含 /video/ 的链接')
    expect(prompt).toContain('前 2 个唯一链接')
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
    expect(prompt).toContain('douyin-top2-<关键词>-<日期>.md')
    expect(prompt).toContain('视频名称作为链接文字')
    expect(prompt).not.toContain('单列')
    expect(prompt).not.toContain('currentSrc')
    expect(prompt).not.toContain('分片下载')
    expect(prompt).not.toContain('comments.jsonl')
  })
})
