// 從製令單勾選後開啟現場報數：每張製令與它目前上工中的線別
export interface PresetWO {
  woNo: string
  lines: { id: string; code: string }[]
}
