import { useState } from 'react'
import { GalleryLayoutSettings } from '../GalleryLayoutSettings'
import { FontSettings } from '../FontSettings'
import { SettingsActionPage } from '../ui/SettingsActionPage'
import { SettingsTaskSwitcher } from '../ui/SettingsTaskSwitcher'

export function DisplaySettingsPage() {
  const [task, setTask] = useState<'cards' | 'reading'>('cards')
  return <SettingsActionPage className="settings-action-display" eyebrow="体验" title="外观" description="选择一项调整，在旁边预览实际效果。">
    <SettingsTaskSwitcher value={task} onChange={setTask} options={[{ value: 'cards', label: '内容卡片' }, { value: 'reading', label: '阅读字体' }]}/>
    <div className="settings-display-workspace">
      {task === 'cards' ? <GalleryLayoutSettings /> : <FontSettings />}
    </div>
  </SettingsActionPage>
}
