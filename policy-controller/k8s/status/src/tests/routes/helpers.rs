use super::make_parent_status;
use crate::index::eq_time_insensitive_route_parent_statuses;

#[test]
fn test_eq_time_insensitive_route_parent_statuses_order_sensitive() {
    // Create RouteParentStatus instances using make_parent_status helper
    let status1 = make_parent_status("ns", "parent1", "Ready", "True", "AllGood");
    let status2 = make_parent_status("ns", "parent2", "Ready", "True", "AllGood");

    // Create two lists with the same elements in different orders
    let list1 = vec![status1.clone(), status2.clone()];
    let list2 = vec![status2, status1];

    // Assert that eq_time_insensitive_route_parent_statuses returns true
    // indicating that it considers the two lists equal
    assert!(eq_time_insensitive_route_parent_statuses(&list1, &list2));
}
