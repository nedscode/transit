import Vue from 'vue'
import Element from 'element-ui'
import enLocale from 'element-ui/lib/locale/lang/en'
import 'element-ui/lib/theme-chalk/index.css'
import App from './App.vue'

Vue.use(Element, {locale: enLocale})
Vue.config.lang = 'en'

new Vue({
  el: '#app',
  render: h => h(App)
})

Vue.nextTick(
    function() {
        document.body.style.display = "block"
    }
)
