import Ember from 'ember';
import config from '../config/environment';

function getLocale() {
 if (navigator.languages !== undefined) {
    return navigator.languages[0];
  } else {
    return navigator.language;
  }
}


export default Ember.Route.extend({
  intl: Ember.inject.service(),

  beforeModel() {
    if (!this._inited) {
      this.get('intl').setLocale(getLocale() || 'en-US');
      this._inited = true
    }
  },

  actions: {
    changeLocale(localeName) {
      this.get('intl').setLocale(localeName);
    }
  },

	model: function() {
    var url = config.APP.ApiUrl + 'api/stats';
    return Ember.$.getJSON(url).then(function(data) {
      return Ember.Object.create(data);
    });
	},

  setupController: function(controller, model) {
    this._super(controller, model);
    Ember.run.later(this, this.refresh, 5000);
  }
});
