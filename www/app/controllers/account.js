import Ember from 'ember';

export default Ember.Controller.extend({
  applicationController: Ember.inject.controller('application'),
  stats: Ember.computed.reads('applicationController.model.stats'),
  nodes: Ember.computed.reads('applicationController'),
  estEarning: Ember.computed('stats', 'model', {
    get() {
      return (4.95 * this.get('model.unpaidShares') / (+this.get('nodes.difficulty'))).toFixed(8);
    }
  }),
  unpaidShareCount: Ember.computed('stats', 'model', {
    get() {
      return this.get('model.unpaidShares') / 5000000000;
    }
  }),
  propPercent: Ember.computed('stats', 'model', {
    get() {
      var percent = this.get('model.roundShares') / this.get('stats.roundShares');
      if (!percent) {
        return 0;
      }
      return percent;
    }
  }),
  roundPercent: Ember.computed('stats', 'model', {
    get() {
      var percent = this.get('model.roundShares') / this.get('stats.roundShares');
      if (!percent) {
        return 0;
      }
      return percent;
    }
  })
});
