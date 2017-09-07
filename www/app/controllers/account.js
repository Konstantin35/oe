import Ember from 'ember';

export default Ember.Controller.extend({
  applicationController: Ember.inject.controller('application'),
  stats: Ember.computed.reads('applicationController.model.stats'),
  nodes: Ember.computed.reads('applicationController'),
  estEarning: Ember.computed('stats', 'model', {
    get() {
      return 4.704 * this.get('model.unpaidShares') / this.get('nodes.difficulty');
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
