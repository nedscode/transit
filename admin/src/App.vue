<template>
  <div id="app">
    <h1>Transit admin</h1>

    <el-table :data="peers">
      <el-table-column label="ID" prop="id" width="160"></el-table-column>
      <el-table-column label="Addr" prop="addr" width="160"></el-table-column>
    </el-table>
  </div>
</template>

<script>
export default {
  data() {
    return {
        cluster: {
            peers: [
                { id: 'self', addr: '127.0.0.1' }
            ]
        }
    }
  },
  methods: {
  },
  mounted() {
    const self = this
    this.$http.get('/admin/api/cluster').then(function(data) {
        self.cluster = data.cluster
    })
  }
}
</script>

<style>
body {
    font: 12pt Helvetica Nue, Helvetica, Verdana, sans-serif;
    color: #555;
    background: #fafafa;
    display: none;
}
</style>
