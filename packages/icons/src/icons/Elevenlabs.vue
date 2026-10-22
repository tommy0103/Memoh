<template>
  <svg
    xmlns="http://www.w3.org/2000/svg"
    :width="size"
    :height="size"
    viewBox="172 129 156 242"
    v-bind="$attrs"
  ><path
     d="M314 355H271V145H314V355Z"
     fill="currentColor"
   />
    <path
      d="M229 355H186V145H229V355Z"
      fill="currentColor"
    /></svg>
</template>

<script setup lang="ts">
withDefaults(defineProps<{ size?: string | number }>(), { size: '1em' })
defineOptions({ inheritAttrs: false })
</script>
