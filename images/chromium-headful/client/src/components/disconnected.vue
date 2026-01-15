<template>
  <div class="disconnected-overlay">
    <div class="message-container">
      <span class="message">{{ $t('disconnected.message') }}</span>
    </div>
  </div>
</template>

<style lang="scss" scoped>
  .disconnected-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba($color: $background-floating, $alpha: 0.95);
    display: flex;
    justify-content: center;
    align-items: center;

    .message-container {
      display: flex;
      justify-content: center;
      align-items: center;
      padding: 20px;

      .message {
        font-size: 18px;
        color: $text-muted;
        text-align: center;
      }
    }
  }
</style>

<script lang="ts">
  import { Component, Vue } from 'vue-property-decorator'

  @Component({ name: 'neko-disconnected' })
  export default class extends Vue {}
</script>
