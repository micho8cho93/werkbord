// The icons the app itself draws (navigation, actions, status), one Lucide icon for each meaning and never the same one twice.
// They are chosen apart from the icons generated workspaces can take (internal/domain/personal_icon_catalog.json), so a
// workspace in the sidebar never looks like a page of the app. app-icons.test.ts keeps all of this true.
import LayoutGrid from '@lucide/svelte/icons/layout-grid';
import LayoutPanelLeft from '@lucide/svelte/icons/layout-panel-left';
import SquareKanban from '@lucide/svelte/icons/square-kanban';
import Calendar from '@lucide/svelte/icons/calendar';
import ChartGantt from '@lucide/svelte/icons/chart-gantt';
import GitFork from '@lucide/svelte/icons/git-fork';
import GitCompareArrows from '@lucide/svelte/icons/git-compare-arrows';
import Radar from '@lucide/svelte/icons/radar';
import SquareTerminal from '@lucide/svelte/icons/square-terminal';
import ChartColumn from '@lucide/svelte/icons/chart-column';
import FolderKanban from '@lucide/svelte/icons/folder-kanban';
import Folder from '@lucide/svelte/icons/folder';
import Library from '@lucide/svelte/icons/library';
import Laptop from '@lucide/svelte/icons/laptop';
import Settings from '@lucide/svelte/icons/settings';
import Pencil from '@lucide/svelte/icons/pencil';
import BadgeQuestionMark from '@lucide/svelte/icons/badge-question-mark';
import SlidersHorizontal from '@lucide/svelte/icons/sliders-horizontal';
import Search from '@lucide/svelte/icons/search';
import Plus from '@lucide/svelte/icons/plus';
import ChevronLeft from '@lucide/svelte/icons/chevron-left';
import ChevronRight from '@lucide/svelte/icons/chevron-right';
import ChevronDown from '@lucide/svelte/icons/chevron-down';
import X from '@lucide/svelte/icons/x';
import Sun from '@lucide/svelte/icons/sun';
import Moon from '@lucide/svelte/icons/moon';
import Play from '@lucide/svelte/icons/play';
import Square from '@lucide/svelte/icons/square';
import Check from '@lucide/svelte/icons/check';
import Reply from '@lucide/svelte/icons/reply';
import GitMerge from '@lucide/svelte/icons/git-merge';
import Copy from '@lucide/svelte/icons/copy';
import Ellipsis from '@lucide/svelte/icons/ellipsis';
import Maximize2 from '@lucide/svelte/icons/maximize-2';
import GitCommitHorizontal from '@lucide/svelte/icons/git-commit-horizontal';
import ExternalLink from '@lucide/svelte/icons/external-link';
import GripVertical from '@lucide/svelte/icons/grip-vertical';
import Users from '@lucide/svelte/icons/users';
import AssistantGlyph from './AssistantGlyph.svelte';
import Paperclip from '@lucide/svelte/icons/paperclip';
import Mic from '@lucide/svelte/icons/mic';
import ArrowUp from '@lucide/svelte/icons/arrow-up';
import ShieldHalf from '@lucide/svelte/icons/shield-half';
import PackageSearch from '@lucide/svelte/icons/package-search';
import FileDiff from '@lucide/svelte/icons/file-diff';
import TextAlignStart from '@lucide/svelte/icons/text-align-start';

export const appIconComponents = {
  control: LayoutGrid,
  overview: LayoutPanelLeft,
  board: SquareKanban,
  calendar: Calendar,
  timeline: ChartGantt,
  git: GitFork,
  'product-map': GitCompareArrows,
  activity: Radar,
  runs: SquareTerminal,
  analytics: ChartColumn,
  projects: FolderKanban,
  folder: Folder,
  database: Library,
  runner: Laptop,
  settings: Settings,
  edit: Pencil,
  help: BadgeQuestionMark,
  defaults: SlidersHorizontal,
  search: Search,
  plus: Plus,
  left: ChevronLeft,
  right: ChevronRight,
  down: ChevronDown,
  close: X,
  sun: Sun,
  moon: Moon,
  play: Play,
  stop: Square,
  check: Check,
  reply: Reply,
  merge: GitMerge,
  copy: Copy,
  more: Ellipsis,
  expand: Maximize2,
  branch: GitCommitHorizontal,
  link: ExternalLink,
  grip: GripVertical,
  people: Users,
  assistant: AssistantGlyph,
  attach: Paperclip,
  mic: Mic,
  send: ArrowUp,
  security: ShieldHalf,
  dependencies: PackageSearch,
  refactor: FileDiff,
  summary: TextAlignStart,
} as const;
export type AppIconName = keyof typeof appIconComponents;

/** The Lucide name behind each icon: the test reads these to check uniqueness against the workspace catalogue. */
export const appIconSources: Record<AppIconName, string> = {
  control: 'layout-grid',
  overview: 'layout-panel-left',
  board: 'square-kanban',
  calendar: 'calendar',
  timeline: 'chart-gantt',
  git: 'git-fork',
  'product-map': 'git-compare-arrows',
  activity: 'radar',
  runs: 'square-terminal',
  analytics: 'chart-column',
  projects: 'folder-kanban',
  folder: 'folder',
  database: 'library',
  runner: 'laptop',
  settings: 'settings',
  edit: 'pencil',
  help: 'badge-question-mark',
  defaults: 'sliders-horizontal',
  search: 'search',
  plus: 'plus',
  left: 'chevron-left',
  right: 'chevron-right',
  down: 'chevron-down',
  close: 'x',
  sun: 'sun',
  moon: 'moon',
  play: 'play',
  stop: 'square',
  check: 'check',
  reply: 'reply',
  merge: 'git-merge',
  copy: 'copy',
  more: 'ellipsis',
  expand: 'maximize-2',
  branch: 'git-commit-horizontal',
  link: 'external-link',
  grip: 'grip-vertical',
  people: 'users',
  assistant: 'werkbord-sparkles',
  attach: 'paperclip',
  mic: 'mic',
  send: 'arrow-up',
  security: 'shield-half',
  dependencies: 'package-search',
  refactor: 'file-diff',
  summary: 'text-align-start',
};
