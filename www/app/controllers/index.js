import Ember from 'ember';

export default Ember.Controller.extend({
  applicationController: Ember.inject.controller('application'),
  stats: Ember.computed.reads('applicationController'),
  config: Ember.computed.reads('applicationController.config'),
	cachedLogin: Ember.computed('login', {
    get() {
      return this.get('login') || Ember.$.cookie('login');
    },
    set(key, value) {
      Ember.$.cookie('login', value);
      this.set('model.login', value);
      return value;
    }
  }),
  dailyReward: Ember.computed('stats', {
    get() {
      return (1000000 / this.get('stats').get('difficulty') * 5 * 0.99 * 24 * 3600).toFixed(8)
    }
  })
  
});
