import { createRouter, createWebHistory } from 'vue-router'
import { getToken } from './api'
import LoginView from './views/LoginView.vue'
import MaterialsView from './views/MaterialsView.vue'
import MaterialDetailView from './views/MaterialDetailView.vue'
import SearchView from './views/SearchView.vue'
import AskView from './views/AskView.vue'
import AgentsView from './views/AgentsView.vue'
import SkillsView from './views/SkillsView.vue'
import ConversationsView from './views/ConversationsView.vue'
import ChatView from './views/ChatView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: LoginView, meta: { public: true } },
    { path: '/', redirect: '/materials' },
    { path: '/materials', name: 'materials', component: MaterialsView },
    { path: '/materials/:id', name: 'material-detail', component: MaterialDetailView, props: true },
    { path: '/search', name: 'search', component: SearchView },
    { path: '/ask', name: 'ask', component: AskView },
    { path: '/agents', name: 'agents', component: AgentsView },
    { path: '/skills', name: 'skills', component: SkillsView },
    { path: '/conversations', name: 'conversations', component: ConversationsView },
    { path: '/chat/:id', name: 'chat', component: ChatView, props: true },
  ],
})

router.beforeEach((to) => {
  const authed = Boolean(getToken())
  if (!to.meta.public && !authed) {
    return { path: '/login' }
  }
  if (to.path === '/login' && authed) {
    return { path: '/materials' }
  }
  return true
})

export default router
