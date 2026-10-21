import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import { setUnauthorizedHandler } from './api'
import './theme.css'
import './style.css'

setUnauthorizedHandler(() => {
  void router.push('/login')
})

createApp(App).use(router).mount('#app')
