'use strict';

/**
 * @ngdoc function
 * @name openshiftConsole.controller:ServicesController
 * @description
 * # ProjectController
 * Controller of the openshiftConsole
 */
angular.module('openshiftConsole')
  .controller('ServicesController', function ($scope, DataService, $filter, LabelFilter) {
    $scope.services = {};
    $scope.unfilteredServices = {};
    $scope.routesByService = {};
    $scope.labelSuggestions = {};
    $scope.alerts = $scope.alerts || {};
    $scope.emptyMessage = "Loading...";
    var watches = [];

    watches.push(DataService.watch("services", $scope, function(services) {
      $scope.unfilteredServices = services.by("metadata.name");
      LabelFilter.addLabelSuggestionsFromResources($scope.unfilteredServices, $scope.labelSuggestions);
      LabelFilter.setLabelSuggestions($scope.labelSuggestions);
      $scope.services = LabelFilter.getLabelSelector().select($scope.unfilteredServices);
      $scope.emptyMessage = "No services to show";
      updateFilterWarning();
      console.log("services (subscribe)", $scope.unfilteredServices);
    }));

    watches.push(DataService.watch("routes", $scope, function(routes){
        $scope.routesByService = routesByService(routes.by("metadata.name"));
        console.log("routes (subscribe)", $scope.routesByService);
    }));

    var routesByService = function(routes) {
        var routeMap = {};
        angular.forEach(routes, function(route, routeName){
            routeMap[route.serviceName] = routeMap[route.serviceName] || {};
            routeMap[route.serviceName][routeName] = route;
        });
        return routeMap;
    };

    var updateFilterWarning = function() {
      if (!LabelFilter.getLabelSelector().isEmpty() && $.isEmptyObject($scope.services)  && !$.isEmptyObject($scope.unfilteredServices)) {
        $scope.alerts["services"] = {
          type: "warning",
          details: "The active filters are hiding all services."
        };
      }
      else {
        delete $scope.alerts["services"];
      }       
    };

    LabelFilter.onActiveFiltersChanged(function(labelSelector) {
      // trigger a digest loop
      $scope.$apply(function() {
        $scope.services = labelSelector.select($scope.unfilteredServices);
        updateFilterWarning();
      });
    });   

    $scope.$on('$destroy', function(){
      DataService.unwatchAll(watches);
    });      
  });
