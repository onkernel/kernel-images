<template>
  <ul>
  </ul>
</template>

<style lang="scss" scoped>
  ul {
    li {
      display: inline-block;
      margin-right: 10px;

      i {
        font-size: 24px;
        cursor: pointer;
      }
    }
  }

  select {
    appearance: none;
    background-color: $background-tertiary;
    border: 1px solid $background-primary;
    color: white;
    cursor: pointer;
    border-radius: 5px;
    height: 24px;
    vertical-align: text-bottom;
    display: inline-block;

    option {
      font-weight: normal;
      color: $text-normal;
      background-color: $background-tertiary;
    }

    &:hover {
      border: 1px solid $background-primary;
    }
  }
</style>

<script lang="ts">
  import { Component, Vue, Watch } from 'vue-property-decorator'
  import { messages } from '~/locale'
  import { set } from '~/utils/localstorage'

  @Component({ name: 'neko-menu' })
  export default class extends Vue {
    get admin() {
      return this.$accessor.user.admin
    }

    get langs() {
      return Object.keys(messages)
    }

    about() {
      this.$accessor.client.toggleAbout()
    }

    @Watch('$i18n.locale')
    onLanguageChange(newLang: string) {
      set('lang', newLang)
    }

    mounted() {
      const default_lang = new URL(location.href).searchParams.get('lang')
      if (default_lang && this.langs.includes(default_lang)) {
        this.$i18n.locale = default_lang
      }
      const show_side = new URL(location.href).searchParams.get('show_side')
      if (show_side !== null) {
        this.$accessor.client.setSide(show_side === '1')
      }
      const mute_chat = new URL(location.href).searchParams.get('mute_chat')
      if (mute_chat !== null) {
        this.$accessor.settings.setSound(mute_chat !== '1')
      }
    }
  }
</script>
