CREATE TABLE favorites (
 id INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
 product_id INT NOT NULL,
 user_id INT NOT NULL,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE KEY unique_user_product (user_id, product_id),
 CONSTRAINT fk_favorites_product FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE,
 CONSTRAINT fk_favorites_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
