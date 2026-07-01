// 配置模块入口
export { default as config } from './project.config'
export {
  projectConfig,
  navigationConfig,
  featuresConfig,
  uiConfig,
} from './project.config'
export { profilePageConfig } from './profile.config'

export type { NavItem, NavSection } from './project.config'
export type {
  AuthorProfileConfig,
  ProfileChannelConfig,
  ProfileIconKey,
  ProjectProfileActionConfig,
  ProjectProfileConfig,
  RemoteAuthorSourceConfig,
  ProfilePageLocalConfig,
} from './profile.config'
