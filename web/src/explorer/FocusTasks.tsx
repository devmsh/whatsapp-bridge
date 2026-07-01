import type { Chat, Circle, Task } from '../api'
import { TasksView } from './TasksView'

// FocusTasks is a thin wrapper over the live TasksView, scoped to a single
// circle. TasksView already filters + renders a flat list when
// selection.kind === 'circle' (see TasksView.tsx: filterByScope /
// groupForRender), so no changes to TasksView are needed here.
export function FocusTasks({
  circleId,
  tasks,
  circles,
  chats,
  nameMap,
  ownJID,
  onOpenTask,
  onCreated,
  onChanged,
}: {
  circleId: number
  tasks: Task[]
  circles: Circle[]
  chats: Chat[]
  nameMap: Map<string, string>
  ownJID: string
  onOpenTask: (id: number) => void
  onCreated: () => void
  onChanged: () => void
}) {
  return (
    <TasksView
      selection={{ kind: 'circle', id: circleId }}
      tasks={tasks}
      circles={circles}
      chats={chats}
      nameMap={nameMap}
      ownJID={ownJID}
      onOpenTask={onOpenTask}
      onCreated={onCreated}
      onChanged={onChanged}
    />
  )
}
